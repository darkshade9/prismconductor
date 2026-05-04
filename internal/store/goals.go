package store

import (
	"database/sql"
	"errors"

	"prismconductor/internal/store/jsonutil"
	"prismconductor/internal/types"
)

// SaveGoal upserts a goal row.
func (s *Store) SaveGoal(g types.Goal) error {
	if s == nil || s.DB == nil {
		return errors.New("store unavailable")
	}
	b, err := jsonutil.Save(nil, g)
	if err != nil {
		return err
	}
	_, err = s.DB.Exec(`
INSERT INTO goals (id, workspace_id, status, json, "order")
VALUES (?, ?, ?, ?, ?)
ON CONFLICT(id) DO UPDATE SET
    workspace_id = excluded.workspace_id,
    status = excluded.status,
    json = excluded.json,
    "order" = excluded."order"`,
		g.ID, nilable(g.WorkspaceID), string(g.Status), string(b), g.Order)
	return err
}

// GetGoal returns a goal by ID.
func (s *Store) GetGoal(id string) (types.Goal, error) {
	var raw string
	err := s.DB.QueryRow(`SELECT json FROM goals WHERE id = ?`, id).Scan(&raw)
	if err != nil {
		return types.Goal{}, err
	}
	var g types.Goal
	if _, err := jsonutil.Load([]byte(raw), &g); err != nil {
		return types.Goal{}, err
	}
	return g, nil
}

// ListGoals returns all goals ordered by status (active first, then backlog,
// then achieved/abandoned) and within each by the manual order column.
func (s *Store) ListGoals() ([]types.Goal, error) {
	if s == nil || s.DB == nil {
		return nil, nil
	}
	rows, err := s.DB.Query(`SELECT json FROM goals
ORDER BY CASE status
    WHEN 'active' THEN 0
    WHEN 'backlog' THEN 1
    WHEN 'achieved' THEN 2
    WHEN 'abandoned' THEN 3
    ELSE 4
END, "order" ASC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []types.Goal
	for rows.Next() {
		var raw string
		if err := rows.Scan(&raw); err != nil {
			return nil, err
		}
		var g types.Goal
		if _, err := jsonutil.Load([]byte(raw), &g); err != nil {
			continue
		}
		out = append(out, g)
	}
	return out, rows.Err()
}

// DeleteGoal removes a goal row.
func (s *Store) DeleteGoal(id string) error {
	if s == nil || s.DB == nil {
		return errors.New("store unavailable")
	}
	_, err := s.DB.Exec(`DELETE FROM goals WHERE id = ?`, id)
	return err
}

// SetGoalActive enforces the §15.4 invariant: at most one active goal at a time.
// Runs in a transaction: clear current active, set new active.
func (s *Store) SetGoalActive(id string) error {
	if s == nil || s.DB == nil {
		return errors.New("store unavailable")
	}
	tx, err := s.DB.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	if err := bumpStatusJSON(tx, "active", "backlog"); err != nil {
		return err
	}
	if err := setGoalStatus(tx, id, "active"); err != nil {
		return err
	}
	return tx.Commit()
}

// SetGoalStatus updates a goal's status. Use SetGoalActive for "active"; this
// is for backlog/achieved/abandoned transitions.
func (s *Store) SetGoalStatus(id string, status types.GoalStatus) error {
	if s == nil || s.DB == nil {
		return errors.New("store unavailable")
	}
	if status == types.GoalActive {
		return s.SetGoalActive(id)
	}
	tx, err := s.DB.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err := setGoalStatus(tx, id, string(status)); err != nil {
		return err
	}
	return tx.Commit()
}

// bumpStatusJSON moves all goals from `from` to `to` and rewrites the JSON's
// status field accordingly.
func bumpStatusJSON(tx *sql.Tx, from, to string) error {
	rows, err := tx.Query(`SELECT id, json FROM goals WHERE status = ?`, from)
	if err != nil {
		return err
	}
	type row struct{ id, raw string }
	var batch []row
	for rows.Next() {
		var r row
		if err := rows.Scan(&r.id, &r.raw); err != nil {
			rows.Close()
			return err
		}
		batch = append(batch, r)
	}
	rows.Close()
	for _, r := range batch {
		var g types.Goal
		rawMap, _ := jsonutil.Load([]byte(r.raw), &g)
		g.Status = types.GoalStatus(to)
		b, _ := jsonutil.Save(rawMap, g)
		if _, err := tx.Exec(`UPDATE goals SET status = ?, json = ? WHERE id = ?`, to, string(b), r.id); err != nil {
			return err
		}
	}
	return nil
}

func setGoalStatus(tx *sql.Tx, id, status string) error {
	var raw string
	if err := tx.QueryRow(`SELECT json FROM goals WHERE id = ?`, id).Scan(&raw); err != nil {
		return err
	}
	var g types.Goal
	rawMap, err := jsonutil.Load([]byte(raw), &g)
	if err != nil {
		return err
	}
	g.Status = types.GoalStatus(status)
	b, _ := jsonutil.Save(rawMap, g)
	_, err = tx.Exec(`UPDATE goals SET status = ?, json = ? WHERE id = ?`, status, string(b), id)
	return err
}

func nilable(s string) any {
	if s == "" {
		return nil
	}
	return s
}
