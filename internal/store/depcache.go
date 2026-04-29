package store

import (
	"encoding/json"
	"errors"
)

// EnsureDepCacheTable creates the dep_cache table if it doesn't exist.
// Called from migrate; exposed so app.startup can call it without a full reset.
func (s *Store) EnsureDepCacheTable() error {
	if s == nil || s.DB == nil {
		return errors.New("store unavailable")
	}
	_, err := s.DB.Exec(`
CREATE TABLE IF NOT EXISTS dep_cache (
    workspace_id TEXT NOT NULL,
    issue_number INTEGER NOT NULL,
    body_hash TEXT NOT NULL,
    goal_id TEXT NOT NULL,
    rank_json TEXT NOT NULL,
    cached_at INTEGER NOT NULL,
    PRIMARY KEY (workspace_id, issue_number, goal_id)
);`)
	return err
}

// DepCache returns the cached rank result for an issue if its body_hash + goal
// match. Empty string return means cache miss.
func (s *Store) DepCacheGet(workspaceID string, issueNumber int, goalID, bodyHash string) (string, bool, error) {
	if s == nil || s.DB == nil {
		return "", false, nil
	}
	var stored, payload string
	err := s.DB.QueryRow(
		`SELECT body_hash, rank_json FROM dep_cache WHERE workspace_id = ? AND issue_number = ? AND goal_id = ?`,
		workspaceID, issueNumber, goalID,
	).Scan(&stored, &payload)
	if err != nil {
		return "", false, nil
	}
	if stored != bodyHash {
		return "", false, nil
	}
	return payload, true, nil
}

// DepCachePut stores a rank result for a (workspace, issue, goal) tuple.
func (s *Store) DepCachePut(workspaceID string, issueNumber int, goalID, bodyHash string, payload any) error {
	if s == nil || s.DB == nil {
		return errors.New("store unavailable")
	}
	b, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	_, err = s.DB.Exec(`
INSERT INTO dep_cache (workspace_id, issue_number, body_hash, goal_id, rank_json, cached_at)
VALUES (?, ?, ?, ?, ?, strftime('%s','now'))
ON CONFLICT(workspace_id, issue_number, goal_id) DO UPDATE SET
    body_hash = excluded.body_hash,
    rank_json = excluded.rank_json,
    cached_at = excluded.cached_at`,
		workspaceID, issueNumber, bodyHash, goalID, string(b))
	return err
}
