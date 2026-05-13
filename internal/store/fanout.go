package store

import (
	"database/sql"
	"encoding/json"
	"errors"
	"time"

	"prismconductor/internal/types"
)

// SaveFanoutProposal upserts a fan-out proposal row. On conflict (same id) it
// updates title, body, labels, and status so the skill can re-emit proposals
// without creating duplicates.
func (s *Store) SaveFanoutProposal(p types.FanoutProposal) error {
	if s == nil || s.DB == nil {
		return errors.New("store unavailable")
	}
	labels, err := json.Marshal(p.Labels)
	if err != nil {
		labels = []byte("[]")
	}
	filedNum := sql.NullInt64{}
	if p.FiledIssueNumber != nil {
		filedNum = sql.NullInt64{Int64: int64(*p.FiledIssueNumber), Valid: true}
	}
	_, err = s.DB.Exec(`
INSERT INTO fanout_proposals
    (id, source_workspace_id, source_issue_number, source_pr_number,
     target_workspace_id, title, body, labels, status,
     filed_issue_number, filed_issue_url, created_at)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
ON CONFLICT(id) DO UPDATE SET
    title              = excluded.title,
    body               = excluded.body,
    labels             = excluded.labels,
    status             = excluded.status,
    filed_issue_number = excluded.filed_issue_number,
    filed_issue_url    = excluded.filed_issue_url`,
		p.ID,
		p.SourceWorkspaceID,
		p.SourceIssueNumber,
		p.SourcePRNumber,
		p.TargetWorkspaceID,
		p.Title,
		p.Body,
		string(labels),
		string(p.Status),
		filedNum,
		p.FiledIssueURL,
		p.CreatedAt.Unix(),
	)
	return err
}

// ListFanoutProposals returns all proposals for the given source (workspace,
// issue) ordered newest-first. Status filter is optional: "" returns all.
func (s *Store) ListFanoutProposals(sourceWorkspaceID string, sourceIssueNumber int) ([]types.FanoutProposal, error) {
	if s == nil || s.DB == nil {
		return nil, errors.New("store unavailable")
	}
	rows, err := s.DB.Query(`
SELECT id, source_workspace_id, source_issue_number, source_pr_number,
       target_workspace_id, title, body, labels, status,
       filed_issue_number, filed_issue_url, created_at
FROM fanout_proposals
WHERE source_workspace_id = ? AND source_issue_number = ?
ORDER BY created_at DESC`,
		sourceWorkspaceID, sourceIssueNumber,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanFanoutProposals(rows)
}

// GetFanoutProposal returns a single proposal by id.
func (s *Store) GetFanoutProposal(id string) (types.FanoutProposal, error) {
	if s == nil || s.DB == nil {
		return types.FanoutProposal{}, errors.New("store unavailable")
	}
	rows, err := s.DB.Query(`
SELECT id, source_workspace_id, source_issue_number, source_pr_number,
       target_workspace_id, title, body, labels, status,
       filed_issue_number, filed_issue_url, created_at
FROM fanout_proposals WHERE id = ?`, id)
	if err != nil {
		return types.FanoutProposal{}, err
	}
	defer rows.Close()
	ps, err := scanFanoutProposals(rows)
	if err != nil {
		return types.FanoutProposal{}, err
	}
	if len(ps) == 0 {
		return types.FanoutProposal{}, errors.New("proposal not found")
	}
	return ps[0], nil
}

// ApproveFanoutProposal marks the proposal as approved and records the filed
// issue details. Idempotent: re-approving the same proposal updates the fields.
func (s *Store) ApproveFanoutProposal(id string, filedIssueNumber int, filedIssueURL string) error {
	if s == nil || s.DB == nil {
		return errors.New("store unavailable")
	}
	_, err := s.DB.Exec(`
UPDATE fanout_proposals
   SET status = 'approved',
       filed_issue_number = ?,
       filed_issue_url    = ?
 WHERE id = ?`, filedIssueNumber, filedIssueURL, id)
	return err
}

// DismissFanoutProposal marks the proposal as dismissed.
func (s *Store) DismissFanoutProposal(id string) error {
	if s == nil || s.DB == nil {
		return errors.New("store unavailable")
	}
	_, err := s.DB.Exec(`UPDATE fanout_proposals SET status = 'dismissed' WHERE id = ?`, id)
	return err
}

// ClearExternalDep clears DependsOnExternal on every issue that has a matching
// source (workspaceID, prNumber). Called by the poller when a source PR merges.
func (s *Store) ClearExternalDep(sourceWorkspaceID string, sourcePRNumber int) error {
	if s == nil || s.DB == nil {
		return errors.New("store unavailable")
	}
	// Read all issues (cross-workspace scan). DependsOnExternal is in the JSON blob.
	rows, err := s.DB.Query(`SELECT workspace_id, number, json FROM issues WHERE depends_on_external IS NOT NULL`)
	if err != nil {
		return err
	}
	defer rows.Close()

	type issueRow struct {
		wsID   string
		number int
		raw    string
	}
	var candidates []issueRow
	for rows.Next() {
		var r issueRow
		if err := rows.Scan(&r.wsID, &r.number, &r.raw); err != nil {
			continue
		}
		candidates = append(candidates, r)
	}
	rows.Close()

	for _, r := range candidates {
		var iss struct {
			DependsOnExternal *types.ExternalDep `json:"depends_on_external,omitempty"`
		}
		if err := json.Unmarshal([]byte(r.raw), &iss); err != nil {
			continue
		}
		dep := iss.DependsOnExternal
		if dep == nil || dep.Resolved {
			continue
		}
		if dep.SourceWorkspaceID != sourceWorkspaceID || dep.SourcePRNumber != sourcePRNumber {
			continue
		}
		// Mark resolved in the JSON blob.
		var full map[string]any
		if err := json.Unmarshal([]byte(r.raw), &full); err != nil {
			continue
		}
		if ext, ok := full["depends_on_external"].(map[string]any); ok {
			ext["resolved"] = true
		}
		updated, err := json.Marshal(full)
		if err != nil {
			continue
		}
		_, _ = s.DB.Exec(`UPDATE issues SET json = ?, depends_on_external = ? WHERE workspace_id = ? AND number = ?`,
			string(updated),
			string(updated), // mirror to the indexed column so future queries hit it
			r.wsID, r.number,
		)
	}
	return nil
}

func scanFanoutProposals(rows *sql.Rows) ([]types.FanoutProposal, error) {
	var out []types.FanoutProposal
	for rows.Next() {
		var p types.FanoutProposal
		var labelsRaw string
		var filedNum sql.NullInt64
		var createdAt int64
		if err := rows.Scan(
			&p.ID,
			&p.SourceWorkspaceID,
			&p.SourceIssueNumber,
			&p.SourcePRNumber,
			&p.TargetWorkspaceID,
			&p.Title,
			&p.Body,
			&labelsRaw,
			&p.Status,
			&filedNum,
			&p.FiledIssueURL,
			&createdAt,
		); err != nil {
			return nil, err
		}
		_ = json.Unmarshal([]byte(labelsRaw), &p.Labels)
		if filedNum.Valid {
			n := int(filedNum.Int64)
			p.FiledIssueNumber = &n
		}
		p.CreatedAt = time.Unix(createdAt, 0)
		out = append(out, p)
	}
	return out, rows.Err()
}
