package store

import (
	"database/sql"
	"encoding/json"
	"errors"
	"path/filepath"
	"strconv"
	"strings"
	"time"

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
INSERT INTO sessions (id, workspace_id, issue_number, pid, state, transcript_path, pending_question_id, json)
VALUES (?, ?, ?, ?, ?, ?, ?, ?)
ON CONFLICT(id) DO UPDATE SET
    pid = excluded.pid,
    state = excluded.state,
    transcript_path = excluded.transcript_path,
    pending_question_id = excluded.pending_question_id,
    json = excluded.json`,
		sess.ID, sess.WorkspaceID, sess.IssueNumber, sess.PID, string(sess.State), transcriptPath, nullableString(sess.PendingQuestionID), string(b))
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

// UpdateSessionPendingQuestion writes the pending_question_id column without
// rewriting the JSON blob. Pass an empty string to clear.
func (s *Store) UpdateSessionPendingQuestion(id, questionID string) error {
	if s == nil || s.DB == nil {
		return nil
	}
	_, err := s.DB.Exec(`UPDATE sessions SET pending_question_id = ? WHERE id = ?`, nullableString(questionID), id)
	return err
}

// UpdateSessionTranscriptOffset persists the byte offset of the last
// fully-processed transcript line (issue #54). Called every ~2s during live
// tail so a conductor restart mid-stream does not re-feed already-processed
// lines.
func (s *Store) UpdateSessionTranscriptOffset(id string, off int64) error {
	if s == nil || s.DB == nil {
		return nil
	}
	_, err := s.DB.Exec(`UPDATE sessions SET transcript_offset = ? WHERE id = ?`, off, id)
	return err
}

// nullableString returns nil for empty strings so the SQLite column stores NULL
// instead of the zero-length string. Keeps `pending_question_id IS NULL` queries
// honest.
func nullableString(s string) any {
	if s == "" {
		return nil
	}
	return s
}

// LoadRunningSessions returns sessions that were running at last shutdown.
// Used for re-attach on startup (§15.3). Includes paused_for_question rows so
// the answer-file watcher can pick them up post-restart (#17).
func (s *Store) LoadRunningSessions() ([]types.Session, string, error) {
	if s == nil || s.DB == nil {
		return nil, "", nil
	}
	rows, err := s.DB.Query(`SELECT json, transcript_path, pending_question_id, transcript_offset FROM sessions WHERE state IN ('running','waiting_for_input','blocked','paused_for_question')`)
	if err != nil {
		return nil, "", err
	}
	defer rows.Close()
	var out []types.Session
	var firstTranscript string
	for rows.Next() {
		var raw, transcript string
		var pendingQID sql.NullString
		var offset int64
		if err := rows.Scan(&raw, &transcript, &pendingQID, &offset); err != nil {
			return nil, "", err
		}
		var sess types.Session
		if err := json.Unmarshal([]byte(raw), &sess); err != nil {
			continue
		}
		// The pending_question_id column is the source of truth — the JSON blob
		// may be older than the most recent UpdateSessionPendingQuestion call.
		if pendingQID.Valid {
			sess.PendingQuestionID = pendingQID.String
		} else {
			sess.PendingQuestionID = ""
		}
		// Issue #54: transcript path lives only on the column, never in the JSON
		// blob. Re-populate it on the in-memory shape so reattach has a path.
		if sess.Transcript == "" {
			sess.Transcript = transcript
		}
		sess.TranscriptOffset = offset
		out = append(out, sess)
		if firstTranscript == "" {
			firstTranscript = transcript
		}
	}
	return out, firstTranscript, rows.Err()
}

// ListPausedForQuestionSessions returns every session row whose state is
// paused_for_question and whose pending_question_id is non-empty (#17). Used
// by the answer-file watcher to find which session a given answer file unlocks.
func (s *Store) ListPausedForQuestionSessions() ([]PausedSession, error) {
	if s == nil || s.DB == nil {
		return nil, nil
	}
	rows, err := s.DB.Query(`
		SELECT id, workspace_id, issue_number, pending_question_id
		FROM sessions
		WHERE state = 'paused_for_question'
		  AND pending_question_id IS NOT NULL
		  AND pending_question_id != ''`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []PausedSession
	for rows.Next() {
		var p PausedSession
		if err := rows.Scan(&p.SessionID, &p.WorkspaceID, &p.IssueNumber, &p.QuestionID); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

// HasRecentFailedPlanSession reports whether the most recent plan-mode
// session for (workspace, issue) ended in a failed/blocked state within the
// given window. Used to gate drag-to-PLAN auto-spawn against silent re-bills
// after an expensive plan blew up — but only while the failure is fresh.
//
// Older history doesn't gate forever; once the window passes, dragging the
// card back to PLAN auto-spawns again. (Otherwise an account that's seen any
// plan failure can never auto-spawn for that issue again, even months later.)
//
// Returns true only when:
//   - a plan session exists for this issue, AND
//   - its state is failed/blocked, AND
//   - its ended_at is within `window` of now.
//
// Live (running/waiting/paused) plan sessions are not this method's job —
// the in-memory active-session check covers them.
func (s *Store) HasRecentFailedPlanSession(workspaceID string, issueNumber int, window time.Duration) (bool, error) {
	if s == nil || s.DB == nil {
		return false, nil
	}
	row := s.DB.QueryRow(`
		SELECT json_extract(json, '$.ended_at'), json_extract(json, '$.state')
		FROM sessions
		WHERE workspace_id = ? AND issue_number = ?
		  AND json_extract(json, '$.mode') = 'plan'
		ORDER BY json_extract(json, '$.started_at') DESC
		LIMIT 1`, workspaceID, issueNumber)
	var endedAt sql.NullString
	var state sql.NullString
	if err := row.Scan(&endedAt, &state); err != nil {
		if err == sql.ErrNoRows {
			return false, nil
		}
		return false, err
	}
	if !state.Valid {
		return false, nil
	}
	if state.String != string(types.StateFailed) && state.String != string(types.StateBlocked) {
		return false, nil
	}
	if !endedAt.Valid || endedAt.String == "" {
		return false, nil
	}
	t, err := time.Parse(time.RFC3339Nano, endedAt.String)
	if err != nil {
		// Couldn't parse the timestamp — be conservative and don't block.
		return false, nil
	}
	return time.Since(t) < window, nil
}

// MostRecentEndedAtForWorktree resolves the most recent terminal-state session
// for the issue whose number is encoded in the worktree directory name
// (`<wsID>-<num>`), returning its EndedAt timestamp.
//
// Used by the 24h GC walk (issue #22) to decide whether a Completed worktree
// is old enough to reclaim. Returns ok=false when:
//   - the path does not encode an issue number,
//   - no terminal session exists for that issue,
//   - the matching session has no EndedAt (state set before the timestamp
//     was persisted; the GC treats this as "unknown, leave alone").
//
// Sessions table has no ended_at column; the timestamp lives only inside the
// JSON blob, so this method unmarshals the row.
func (s *Store) MostRecentEndedAtForWorktree(workspaceID, worktreePath string) (time.Time, bool, error) {
	if s == nil || s.DB == nil {
		return time.Time{}, false, errors.New("store unavailable")
	}
	base := filepath.Base(worktreePath)
	idx := strings.LastIndex(base, "-")
	if idx < 0 || idx == len(base)-1 {
		return time.Time{}, false, nil
	}
	num, err := strconv.Atoi(base[idx+1:])
	if err != nil {
		return time.Time{}, false, nil
	}
	var raw string
	err = s.DB.QueryRow(
		`SELECT json FROM sessions
		 WHERE workspace_id = ? AND issue_number = ?
		   AND state IN ('completed','failed','blocked')
		 ORDER BY rowid DESC LIMIT 1`,
		workspaceID, num,
	).Scan(&raw)
	if err != nil {
		return time.Time{}, false, nil
	}
	var sess types.Session
	if err := json.Unmarshal([]byte(raw), &sess); err != nil {
		return time.Time{}, false, err
	}
	if sess.EndedAt == nil {
		return time.Time{}, false, nil
	}
	return *sess.EndedAt, true, nil
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
