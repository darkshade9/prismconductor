package store

import (
	"database/sql"
	"encoding/json"
	"errors"

	"prismconductor/internal/types"
)

// SavePlan upserts a plan revision.
func (s *Store) SavePlan(p types.Plan) error {
	if s == nil || s.DB == nil {
		return errors.New("store unavailable")
	}
	b, err := json.Marshal(p)
	if err != nil {
		return err
	}
	_, err = s.DB.Exec(`
INSERT INTO plans (workspace_id, issue_number, revision, json)
VALUES (?, ?, ?, ?)
ON CONFLICT(workspace_id, issue_number, revision) DO UPDATE SET
    json = excluded.json`,
		p.WorkspaceID, p.IssueNumber, p.Revision, string(b))
	return err
}

// GetPlan returns a specific revision.
func (s *Store) GetPlan(workspaceID string, issueNumber, revision int) (types.Plan, error) {
	var raw string
	err := s.DB.QueryRow(`SELECT json FROM plans WHERE workspace_id = ? AND issue_number = ? AND revision = ?`,
		workspaceID, issueNumber, revision).Scan(&raw)
	if err != nil {
		return types.Plan{}, err
	}
	var p types.Plan
	if err := json.Unmarshal([]byte(raw), &p); err != nil {
		return types.Plan{}, err
	}
	return p, nil
}

// PendingPlan is the lightweight payload returned by ListPendingPlans —
// enough for the UI to mark a card as "plan ready (rev N)" without loading
// the full plan markdown.
type PendingPlan struct {
	WorkspaceID  string `json:"workspace_id"`
	IssueNumber  int    `json:"issue_number"`
	Revision     int    `json:"revision"`
}

// ListPendingPlans returns the latest plan revision for every issue whose
// most-recent plan has NOT been approved yet (approved_at null in JSON).
// Used at app startup to rehydrate the planReady glow on cards that had a
// plan ready when the user last quit.
func (s *Store) ListPendingPlans() ([]PendingPlan, error) {
	if s == nil || s.DB == nil {
		return nil, nil
	}
	// Pick the highest revision per (workspace, issue) and inspect its JSON.
	rows, err := s.DB.Query(`
SELECT p.workspace_id, p.issue_number, p.revision, p.json
FROM plans p
JOIN (
    SELECT workspace_id, issue_number, MAX(revision) AS max_rev
    FROM plans
    GROUP BY workspace_id, issue_number
) latest ON p.workspace_id = latest.workspace_id
       AND p.issue_number = latest.issue_number
       AND p.revision = latest.max_rev
LEFT JOIN issues i ON p.workspace_id = i.workspace_id
                  AND p.issue_number = i.number
WHERE i.column_name IS NULL OR i.column_name NOT IN ('done','review')`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []PendingPlan
	for rows.Next() {
		var pp PendingPlan
		var raw string
		if err := rows.Scan(&pp.WorkspaceID, &pp.IssueNumber, &pp.Revision, &raw); err != nil {
			return nil, err
		}
		var plan types.Plan
		if err := json.Unmarshal([]byte(raw), &plan); err != nil {
			continue
		}
		if plan.ApprovedAt == nil {
			out = append(out, pp)
		}
	}
	return out, rows.Err()
}

// LatestPlan returns the highest-revision plan for an issue, or nil if none.
func (s *Store) LatestPlan(workspaceID string, issueNumber int) (*types.Plan, error) {
	if s == nil || s.DB == nil {
		return nil, nil
	}
	var raw string
	err := s.DB.QueryRow(`SELECT json FROM plans WHERE workspace_id = ? AND issue_number = ? ORDER BY revision DESC LIMIT 1`,
		workspaceID, issueNumber).Scan(&raw)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var p types.Plan
	if err := json.Unmarshal([]byte(raw), &p); err != nil {
		return nil, err
	}
	return &p, nil
}

// ListPlans returns every revision for an issue, oldest first.
func (s *Store) ListPlans(workspaceID string, issueNumber int) ([]types.Plan, error) {
	if s == nil || s.DB == nil {
		return nil, nil
	}
	rows, err := s.DB.Query(`SELECT json FROM plans WHERE workspace_id = ? AND issue_number = ? ORDER BY revision ASC`,
		workspaceID, issueNumber)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []types.Plan
	for rows.Next() {
		var raw string
		if err := rows.Scan(&raw); err != nil {
			return nil, err
		}
		var p types.Plan
		if err := json.Unmarshal([]byte(raw), &p); err != nil {
			continue
		}
		out = append(out, p)
	}
	return out, rows.Err()
}
