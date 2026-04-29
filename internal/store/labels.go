package store

import (
	"errors"

	"prismconductor/internal/types"
)

// SaveLabels replaces the full label set for a workspace in a single
// transaction (Q5 single-TX replace-all).
func (s *Store) SaveLabels(workspaceID string, labels []types.Label) error {
	if s == nil || s.DB == nil {
		return errors.New("store unavailable")
	}
	tx, err := s.DB.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.Exec(`DELETE FROM labels WHERE workspace_id = ?`, workspaceID); err != nil {
		return err
	}
	stmt, err := tx.Prepare(`INSERT INTO labels (workspace_id, name, color, description) VALUES (?, ?, ?, ?)`)
	if err != nil {
		return err
	}
	defer stmt.Close()
	for _, l := range labels {
		if _, err := stmt.Exec(workspaceID, l.Name, l.Color, l.Description); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// ListLabels returns the cached label set for a workspace, sorted by name.
func (s *Store) ListLabels(workspaceID string) ([]types.Label, error) {
	if s == nil || s.DB == nil {
		return nil, nil
	}
	rows, err := s.DB.Query(
		`SELECT name, color, COALESCE(description, '') FROM labels WHERE workspace_id = ? ORDER BY name`,
		workspaceID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []types.Label
	for rows.Next() {
		var l types.Label
		if err := rows.Scan(&l.Name, &l.Color, &l.Description); err != nil {
			return nil, err
		}
		out = append(out, l)
	}
	return out, rows.Err()
}

// EnsureLabelsTable is a no-op once migrate() has run; kept as a safety net for
// existing DBs that pre-date the labels table.
func (s *Store) EnsureLabelsTable() error {
	if s == nil || s.DB == nil {
		return errors.New("store unavailable")
	}
	_, err := s.DB.Exec(`CREATE TABLE IF NOT EXISTS labels (
    workspace_id TEXT NOT NULL,
    name         TEXT NOT NULL,
    color        TEXT NOT NULL,
    description  TEXT,
    PRIMARY KEY (workspace_id, name)
)`)
	return err
}

