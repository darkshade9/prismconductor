package store

import (
	"errors"
	"time"

	"prismconductor/internal/types"
)

// EnqueuePendingPool records that the given issue is waiting for a free slot
// of the specified role. The UNIQUE constraint silently de-duplicates
// concurrent enqueue attempts for the same (workspace, issue, role, action).
func (s *Store) EnqueuePendingPool(workspaceID string, issueNumber int, role types.Role, action string) error {
	if s == nil || s.DB == nil {
		return errors.New("store unavailable")
	}
	_, err := s.DB.Exec(`
INSERT INTO pending_pool_for (workspace_id, issue_number, role, action, created_at)
VALUES (?, ?, ?, ?, ?)
ON CONFLICT(workspace_id, issue_number, role, action) DO NOTHING`,
		workspaceID, issueNumber, string(role), action, time.Now().Unix())
	return err
}

// DequeuePendingPool removes a single pending row by primary key.
func (s *Store) DequeuePendingPool(id int64) error {
	if s == nil || s.DB == nil {
		return errors.New("store unavailable")
	}
	_, err := s.DB.Exec(`DELETE FROM pending_pool_for WHERE id = ?`, id)
	return err
}

// ListPendingPools returns up to limit pending requests ordered FIFO.
func (s *Store) ListPendingPools(limit int) ([]types.PendingPoolRequest, error) {
	if s == nil || s.DB == nil {
		return nil, errors.New("store unavailable")
	}
	if limit <= 0 {
		limit = 100
	}
	rows, err := s.DB.Query(`
SELECT id, workspace_id, issue_number, role, action, created_at
FROM pending_pool_for
ORDER BY created_at ASC, id ASC
LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []types.PendingPoolRequest
	for rows.Next() {
		var req types.PendingPoolRequest
		var role string
		var createdAt int64
		if err := rows.Scan(&req.ID, &req.WorkspaceID, &req.IssueNumber, &role, &req.Action, &createdAt); err != nil {
			return nil, err
		}
		req.Role = types.Role(role)
		req.CreatedAt = time.Unix(createdAt, 0)
		out = append(out, req)
	}
	return out, rows.Err()
}

// DeletePendingForIssue removes all pending rows for a given issue.
// Called when a spawn succeeds or when an issue is explicitly cancelled.
func (s *Store) DeletePendingForIssue(workspaceID string, issueNumber int) error {
	if s == nil || s.DB == nil {
		return errors.New("store unavailable")
	}
	_, err := s.DB.Exec(
		`DELETE FROM pending_pool_for WHERE workspace_id = ? AND issue_number = ?`,
		workspaceID, issueNumber)
	return err
}
