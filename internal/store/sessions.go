package store

import (
	"encoding/json"
	"errors"

	"prismconductor/internal/types"
)

// SaveSession upserts a session row.
func (s *Store) SaveSession(sess *types.Session, transcriptPath string) error {
	if s == nil || s.DB == nil {
		return errors.New("store unavailable")
	}
	b, err := json.Marshal(sess)
	if err != nil {
		return err
	}
	_, err = s.DB.Exec(`
INSERT INTO sessions (id, workspace_id, issue_number, pid, state, transcript_path, json)
VALUES (?, ?, ?, ?, ?, ?, ?)
ON CONFLICT(id) DO UPDATE SET
    pid = excluded.pid,
    state = excluded.state,
    transcript_path = excluded.transcript_path,
    json = excluded.json`,
		sess.ID, sess.WorkspaceID, sess.IssueNumber, sess.PID, string(sess.State), transcriptPath, string(b))
	return err
}

// UpdateSessionState updates only the live fields without rewriting the JSON blob.
func (s *Store) UpdateSessionState(id string, state types.SessionState) error {
	if s == nil || s.DB == nil {
		return nil
	}
	_, err := s.DB.Exec(`UPDATE sessions SET state = ? WHERE id = ?`, string(state), id)
	return err
}

// LoadRunningSessions returns sessions that were running at last shutdown.
// Used for re-attach on startup (§15.3).
func (s *Store) LoadRunningSessions() ([]types.Session, string, error) {
	if s == nil || s.DB == nil {
		return nil, "", nil
	}
	rows, err := s.DB.Query(`SELECT json, transcript_path FROM sessions WHERE state IN ('running','waiting_for_input','blocked')`)
	if err != nil {
		return nil, "", err
	}
	defer rows.Close()
	var out []types.Session
	var firstTranscript string
	for rows.Next() {
		var raw, transcript string
		if err := rows.Scan(&raw, &transcript); err != nil {
			return nil, "", err
		}
		var sess types.Session
		if err := json.Unmarshal([]byte(raw), &sess); err != nil {
			continue
		}
		out = append(out, sess)
		if firstTranscript == "" {
			firstTranscript = transcript
		}
	}
	return out, firstTranscript, rows.Err()
}

// SessionTranscriptPath returns the spool path for a session id.
func (s *Store) SessionTranscriptPath(id string) (string, error) {
	if s == nil || s.DB == nil {
		return "", errors.New("store unavailable")
	}
	var path string
	err := s.DB.QueryRow(`SELECT transcript_path FROM sessions WHERE id = ?`, id).Scan(&path)
	return path, err
}
