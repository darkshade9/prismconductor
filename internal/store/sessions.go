package store

import (
	"database/sql"
	"errors"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"prismconductor/internal/store/jsonutil"
	"prismconductor/internal/types"
)

// SaveSession upserts a session row.
func (s *Store) SaveSession(sess *types.Session, transcriptPath string) error {
	if s == nil || s.DB == nil {
		return errors.New("store unavailable")
	}
	b, err := jsonutil.Save(nil, sess)
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

// UpdateSessionState atomically updates both the indexed `state` column AND
// the corresponding `$.state` field inside the JSON blob.
//
// Why patch both: ListSessionsForIssue (and every other reader that
// unmarshals the json blob) reads state from `$.state`, NOT from the
// indexed column. The historical implementation updated only the indexed
// column, leaving the JSON's `state` frozen at whatever value SaveSession
// last serialized — typically "running". Result: every session whose state
// transitioned via UpdateSessionState (the live-tail terminal path) ended
// up with indexed state in {completed, failed, blocked, …} but json.state
// = "running". The IssueView assembler then surfaced these as "active"
// forever, which is exactly the zombie-glow class of bug.
//
// Use json_set so SQLite does the patch atomically inside one statement;
// no read-modify-write race window where a concurrent UnmarshalJSON could
// see a half-baked blob.
func (s *Store) UpdateSessionState(id string, state types.SessionState) error {
	if s == nil || s.DB == nil {
		return nil
	}
	_, err := s.DB.Exec(
		`UPDATE sessions
		   SET state = ?,
		       json  = json_set(json, '$.state', ?)
		 WHERE id = ?`,
		string(state), string(state), id)
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
// Acknowledged blocked/failed sessions (issue #88) are excluded — they were
// cleared by the user and should not resurrect the red overlay on restart.
func (s *Store) LoadRunningSessions() ([]types.Session, string, error) {
	if s == nil || s.DB == nil {
		return nil, "", nil
	}
	rows, err := s.DB.Query(`SELECT json, transcript_path, pending_question_id, transcript_offset FROM sessions WHERE state IN ('running','waiting_for_input','blocked','paused_for_question') AND (acknowledged_at IS NULL OR acknowledged_at = 0)`)
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
		if _, err := jsonutil.Load([]byte(raw), &sess); err != nil {
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

// RecentTerminalSessions returns the most-recent terminal-state session per
// (workspace, issue). Used at app startup to rehydrate the frontend's
// session store with prior failures so the lastFailure overlay survives a
// restart instead of silently disappearing every time the user closes the
// app. Caps at `limit` rows total to keep payload bounded.
func (s *Store) RecentTerminalSessions(limit int) ([]types.Session, error) {
	if s == nil || s.DB == nil {
		return nil, nil
	}
	if limit <= 0 {
		limit = 50
	}
	// One row per (workspace, issue): the latest started_at among terminal
	// states, excluding acknowledged sessions (issue #88's clear path) so the
	// red overlay doesn't resurrect on restart after the user dismissed it.
	rows, err := s.DB.Query(`
WITH ranked AS (
    SELECT id, json, transcript_path, transcript_offset,
           ROW_NUMBER() OVER (
               PARTITION BY workspace_id, issue_number
               ORDER BY json_extract(json, '$.started_at') DESC
           ) AS rn
    FROM sessions
    WHERE state IN ('completed','failed','blocked')
      AND (acknowledged_at IS NULL OR acknowledged_at = 0)
)
SELECT json, transcript_path, transcript_offset
FROM ranked
WHERE rn = 1
ORDER BY json_extract(json, '$.started_at') DESC
LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []types.Session
	for rows.Next() {
		var raw, transcript string
		var offset int64
		if err := rows.Scan(&raw, &transcript, &offset); err != nil {
			return nil, err
		}
		var sess types.Session
		if _, err := jsonutil.Load([]byte(raw), &sess); err != nil {
			continue
		}
		if sess.Transcript == "" {
			sess.Transcript = transcript
		}
		sess.TranscriptOffset = offset
		out = append(out, sess)
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
		  AND (acknowledged_at IS NULL OR acknowledged_at = 0)
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
	if _, err := jsonutil.Load([]byte(raw), &sess); err != nil {
		return time.Time{}, false, err
	}
	if sess.EndedAt == nil {
		return time.Time{}, false, nil
	}
	return *sess.EndedAt, true, nil
}

// UpdateSessionUsage persists per-session token counts and estimated cost to
// the dedicated columns added in issue #101. Also updates the JSON blob so
// ListSessionsForIssue returns the token data for IssueView assembly and
// the GC end-timestamp path.
func (s *Store) UpdateSessionUsage(sess *types.Session, transcriptPath string) error {
	if s == nil || s.DB == nil {
		return nil
	}
	b, err := jsonutil.Save(nil, sess)
	if err != nil {
		return err
	}
	_, err = s.DB.Exec(
		`UPDATE sessions SET
			input_tokens = ?,
			output_tokens = ?,
			estimated_cost_cents = ?,
			state = ?,
			json = ?
		 WHERE id = ?`,
		sess.InputTokens, sess.OutputTokens, sess.EstimatedCostCents,
		string(sess.State), string(b), sess.ID)
	return err
}

// PoolSpendCents returns the sum of estimated_cost_cents for all terminal
// sessions on the given pool since the supplied time. Used for per-pool
// spend aggregates in Settings → Pools (issue #101).
func (s *Store) PoolSpendCents(poolID string, since time.Time) (float64, error) {
	if s == nil || s.DB == nil {
		return 0, nil
	}
	var total float64
	err := s.DB.QueryRow(
		`SELECT COALESCE(SUM(estimated_cost_cents), 0)
		 FROM sessions
		 WHERE json_extract(json, '$.pool_id') = ?
		   AND json_extract(json, '$.started_at') >= ?
		   AND state IN ('completed','failed','blocked')`,
		poolID, since.UTC().Format(time.RFC3339)).Scan(&total)
	return total, err
}

// CountRunningSessionsByPool returns a map[poolID]count of every session
// currently in `running` or `waiting_for_input` state, keyed by the
// session's pool_id.
//
// Used by the Registry to reconcile its in-memory `active` counter against
// DB ground truth. Without this reconciliation, any acquire-without-matching-
// release leaks the slot in the registry forever (witnessed: planner
// counter stuck at 1/2 with zero plan sessions actually running). The DB is
// the source of truth; the registry counter is now a cache derived from
// this query at startup and on every terminal session transition.
//
// Reads json_extract on $.pool_id rather than a dedicated column because
// PoolID is stored only inside the session JSON blob, not in an indexed
// column. SQLite's json1 extension handles this efficiently for the small
// active-session row count we ever have (sub-100 rows).
func (s *Store) CountRunningSessionsByPool() (map[string]int, error) {
	if s == nil || s.DB == nil {
		return nil, nil
	}
	rows, err := s.DB.Query(
		`SELECT json_extract(json, '$.pool_id') AS pool_id, COUNT(*)
		   FROM sessions
		  WHERE state IN ('running','waiting_for_input')
		    AND json_extract(json, '$.pool_id') IS NOT NULL
		    AND json_extract(json, '$.pool_id') <> ''
		  GROUP BY json_extract(json, '$.pool_id')`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]int{}
	for rows.Next() {
		var poolID string
		var count int
		if err := rows.Scan(&poolID, &count); err != nil {
			return nil, err
		}
		out[poolID] = count
	}
	return out, rows.Err()
}

// GoalSpendCents returns the (totalCents, runCount) for all terminal sessions
// belonging to issues assigned to the given goal since the supplied time
// (issue #101).
func (s *Store) GoalSpendCents(goalID string, since time.Time) (float64, int, error) {
	if s == nil || s.DB == nil {
		return 0, 0, nil
	}
	row := s.DB.QueryRow(
		`SELECT COALESCE(SUM(s.estimated_cost_cents), 0), COUNT(*)
		 FROM sessions s
		 JOIN issues i
		   ON s.workspace_id = i.workspace_id AND s.issue_number = i.number
		 WHERE json_extract(i.json, '$.goal_id') = ?
		   AND json_extract(s.json, '$.started_at') >= ?
		   AND s.state IN ('completed','failed','blocked')`,
		goalID, since.UTC().Format(time.RFC3339))
	var totalCents float64
	var count int
	if err := row.Scan(&totalCents, &count); err != nil {
		return 0, 0, err
	}
	return totalCents, count, nil
}

// WorkspaceSpendCents returns the sum of estimated_cost_cents for all terminal
// sessions in the given workspace since the supplied time (issue #101).
func (s *Store) WorkspaceSpendCents(workspaceID string, since time.Time) (float64, error) {
	if s == nil || s.DB == nil {
		return 0, nil
	}
	var total float64
	err := s.DB.QueryRow(
		`SELECT COALESCE(SUM(estimated_cost_cents), 0)
		 FROM sessions
		 WHERE workspace_id = ?
		   AND json_extract(json, '$.started_at') >= ?
		   AND state IN ('completed','failed','blocked')`,
		workspaceID, since.UTC().Format(time.RFC3339)).Scan(&total)
	return total, err
}

// UpdateSessionDiagnostics persists subprocess exit diagnostics for failed
// execute sessions (#194). Written only after session teardown.
func (s *Store) UpdateSessionDiagnostics(id string, exitCode *int, signal string, waitMs int64, lastStderr string) error {
	if s == nil || s.DB == nil {
		return nil
	}
	var codeVal any
	if exitCode != nil {
		codeVal = *exitCode
	}
	_, err := s.DB.Exec(
		`UPDATE sessions SET
			exit_code = ?,
			signal = ?,
			cmd_wait_time_ms = ?,
			last_stderr_chunk = ?
		 WHERE id = ?`,
		codeVal, nullableString(signal), waitMs, nullableString(lastStderr), id)
	return err
}

// MostRecentFailedExecuteSession returns the most recent unacknowledged failed
// or blocked execute session with a branch name for (workspaceID, issueNumber).
// Used by the orphan-PR poller to find pushed branches with no open PR (#194).
func (s *Store) MostRecentFailedExecuteSession(workspaceID string, issueNumber int) (*types.Session, error) {
	if s == nil || s.DB == nil {
		return nil, nil
	}
	var raw string
	err := s.DB.QueryRow(`
		SELECT json FROM sessions
		WHERE workspace_id = ? AND issue_number = ?
		  AND state IN ('failed','blocked')
		  AND json_extract(json, '$.mode') = 'execute'
		  AND json_extract(json, '$.branch') IS NOT NULL
		  AND json_extract(json, '$.branch') <> ''
		  AND (acknowledged_at IS NULL OR acknowledged_at = 0)
		ORDER BY json_extract(json, '$.started_at') DESC
		LIMIT 1`, workspaceID, issueNumber).Scan(&raw)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var sess types.Session
	if _, err := jsonutil.Load([]byte(raw), &sess); err != nil {
		return nil, err
	}
	return &sess, nil
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

// ListSessionsForIssue returns all sessions for (workspaceID, issueNumber),
// ordered by started_at descending. Used by the IssueView assembler to derive
// activeSession, pausedSession, lastFailure, and the pool-badge source.
func (s *Store) ListSessionsForIssue(workspaceID string, issueNumber int) ([]types.Session, error) {
	if s == nil || s.DB == nil {
		return nil, nil
	}
	rows, err := s.DB.Query(
		`SELECT json, pending_question_id FROM sessions
		 WHERE workspace_id = ? AND issue_number = ?
		 ORDER BY json_extract(json, '$.started_at') DESC`,
		workspaceID, issueNumber)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []types.Session
	for rows.Next() {
		var raw string
		var pendingQID sql.NullString
		if err := rows.Scan(&raw, &pendingQID); err != nil {
			return nil, err
		}
		var sess types.Session
		if _, err := jsonutil.Load([]byte(raw), &sess); err != nil {
			continue
		}
		if pendingQID.Valid {
			sess.PendingQuestionID = pendingQID.String
		}
		out = append(out, sess)
	}
	return out, rows.Err()
}

// TerminateOrphanPausedSession finds the most-recent unacknowledged
// paused_for_question session for (workspaceID, issueNumber), marks it as
// failed with the given reason, stamps acknowledged_at, and returns the
// updated session. Returns nil (no error) if no matching row exists.
// Used by RecoverOrphanQuestion and the auto-recovery watcher (#153).
func (s *Store) TerminateOrphanPausedSession(workspaceID string, issueNumber int, reason string) (*types.Session, error) {
	if s == nil || s.DB == nil {
		return nil, errors.New("store unavailable")
	}
	now := time.Now()
	nowUnix := now.Unix()
	var id, raw string
	err := s.DB.QueryRow(`
		SELECT id, json FROM sessions
		WHERE workspace_id = ? AND issue_number = ?
		  AND state = 'paused_for_question'
		  AND (acknowledged_at IS NULL OR acknowledged_at = 0)
		ORDER BY json_extract(json, '$.started_at') DESC
		LIMIT 1`, workspaceID, issueNumber).Scan(&id, &raw)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var sess types.Session
	rawMap, err := jsonutil.Load([]byte(raw), &sess)
	if err != nil {
		return nil, err
	}
	sess.State = types.StateFailed
	sess.BlockedReason = reason
	sess.EndedAt = &now
	sess.AcknowledgedAt = &nowUnix
	updated, err := jsonutil.Save(rawMap, &sess)
	if err != nil {
		return nil, err
	}
	_, err = s.DB.Exec(`
		UPDATE sessions
		   SET state = 'failed',
		       json = ?,
		       acknowledged_at = ?
		 WHERE id = ?`,
		string(updated), nowUnix, id)
	if err != nil {
		return nil, err
	}
	return &sess, nil
}

// AcknowledgeLatestFailure marks the most recent unacknowledged blocked/failed
// session for the given issue as acknowledged (issue #88). The session row is
// preserved for audit; only acknowledged_at is set so the UI stops showing the
// blocked overlay. Returns nil session (no error) when no matching row exists.
func (s *Store) AcknowledgeLatestFailure(workspaceID string, issueNumber int) (*types.Session, error) {
	if s == nil || s.DB == nil {
		return nil, errors.New("store unavailable")
	}
	now := time.Now().Unix()
	var id, raw string
	// Check BOTH the indexed `state` column AND the json blob's `$.state`
	// because the two could be out of sync if a row was written by an
	// older binary before the json_set unification (or if a future bug
	// drifts them again). The Clear Failure button must clear the
	// failure regardless of which source surfaced it; any other behavior
	// makes the button look broken to the user.
	err := s.DB.QueryRow(`
		SELECT id, json FROM sessions
		WHERE workspace_id = ? AND issue_number = ?
		  AND (state IN ('blocked','failed')
		       OR json_extract(json, '$.state') IN ('blocked','failed'))
		  AND (acknowledged_at IS NULL OR acknowledged_at = 0)
		ORDER BY json_extract(json, '$.started_at') DESC
		LIMIT 1`, workspaceID, issueNumber).Scan(&id, &raw)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var sess types.Session
	rawMap, err2 := jsonutil.Load([]byte(raw), &sess)
	if err2 != nil {
		return nil, err2
	}
	sess.AcknowledgedAt = &now
	updated, err := jsonutil.Save(rawMap, &sess)
	if err != nil {
		return nil, err
	}
	_, err = s.DB.Exec(`UPDATE sessions SET acknowledged_at = ?, json = ? WHERE id = ?`,
		now, string(updated), id)
	if err != nil {
		return nil, err
	}
	return &sess, nil
}

// BackfillMissingFailureCause sets FailureCause.Kind = "unknown_pre_card_state_v2"
// on every terminal failed/blocked session row whose JSON blob has no
// failure_cause field. Called once at startup so the UI never renders a
// null failure reason for legacy rows (issue #30 q3).
//
// Returns the number of rows updated.
func (s *Store) BackfillMissingFailureCause() (int, error) {
	if s == nil || s.DB == nil {
		return 0, errors.New("store unavailable")
	}
	rows, err := s.DB.Query(
		`SELECT id, json FROM sessions
		 WHERE state IN ('failed', 'blocked')
		   AND json_extract(json, '$.failure_cause') IS NULL`)
	if err != nil {
		return 0, err
	}
	defer rows.Close()

	type candidate struct {
		id  string
		raw string
	}
	var candidates []candidate
	for rows.Next() {
		var c candidate
		if err := rows.Scan(&c.id, &c.raw); err != nil {
			return 0, err
		}
		candidates = append(candidates, c)
	}
	if err := rows.Err(); err != nil {
		return 0, err
	}

	updated := 0
	cause := &types.FailureCause{Kind: "unknown_pre_card_state_v2"}
	for _, c := range candidates {
		var sess types.Session
		rawMap, err := jsonutil.Load([]byte(c.raw), &sess)
		if err != nil {
			continue
		}
		sess.FailureCause = cause
		b, err := jsonutil.Save(rawMap, &sess)
		if err != nil {
			continue
		}
		if _, err := s.DB.Exec(`UPDATE sessions SET json = ? WHERE id = ?`, string(b), c.id); err != nil {
			continue
		}
		updated++
	}
	return updated, nil
}
