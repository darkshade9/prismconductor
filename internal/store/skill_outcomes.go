package store

import (
	"time"

	"prismconductor/internal/types"
)

// RecordSkillOutcome writes (or upserts) one skill outcome row. Idempotent on
// the same session_id so double-delivery from handleSessionStateChange is safe.
func (s *Store) RecordSkillOutcome(o types.SkillOutcome) error {
	_, err := s.DB.Exec(`
		INSERT INTO skill_outcomes
		    (session_id, workspace_id, issue_number, skill_path, skill_hash,
		     mode, outcome, blocked_reason, user_action,
		     cost_cents, duration_ms, transcript_path, captured_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(session_id) DO UPDATE SET
		    outcome         = excluded.outcome,
		    blocked_reason  = excluded.blocked_reason,
		    cost_cents      = excluded.cost_cents,
		    duration_ms     = excluded.duration_ms,
		    captured_at     = excluded.captured_at`,
		o.SessionID, o.WorkspaceID, o.IssueNumber,
		o.SkillPath, o.SkillHash, o.Mode, o.Outcome,
		o.BlockedReason, o.UserAction,
		o.CostCents, o.DurationMs, o.TranscriptPath, o.CapturedAt,
	)
	return err
}

// ListSkillOutcomes returns outcomes for a skill path since sinceUnix (epoch
// seconds), newest-first, capped at limit rows. Pass limit=0 for no cap.
func (s *Store) ListSkillOutcomes(skillPath string, sinceUnix int64, limit int) ([]types.SkillOutcome, error) {
	q := `SELECT session_id, workspace_id, issue_number, skill_path, skill_hash,
	             mode, outcome, blocked_reason, user_action,
	             cost_cents, duration_ms, transcript_path, captured_at
	        FROM skill_outcomes
	       WHERE skill_path = ? AND captured_at >= ?
	       ORDER BY captured_at DESC`
	args := []any{skillPath, sinceUnix}
	if limit > 0 {
		q += " LIMIT ?"
		args = append(args, limit)
	}
	rows, err := s.DB.Query(q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanSkillOutcomes(rows)
}

// SummarizeOutcomes returns aggregate outcome counts for a skill path since
// sinceUnix (epoch seconds).
func (s *Store) SummarizeOutcomes(skillPath string, sinceUnix int64) (types.SkillOutcomeSummary, error) {
	rows, err := s.DB.Query(`
		SELECT outcome, COUNT(*)
		  FROM skill_outcomes
		 WHERE skill_path = ? AND captured_at >= ?
		 GROUP BY outcome`,
		skillPath, sinceUnix,
	)
	if err != nil {
		return types.SkillOutcomeSummary{}, err
	}
	defer rows.Close()

	counts := make(map[string]int)
	total := 0
	for rows.Next() {
		var outcome string
		var n int
		if err := rows.Scan(&outcome, &n); err != nil {
			return types.SkillOutcomeSummary{}, err
		}
		counts[outcome] = n
		total += n
	}
	if err := rows.Err(); err != nil {
		return types.SkillOutcomeSummary{}, err
	}
	return types.SkillOutcomeSummary{
		SkillPath:     skillPath,
		TotalSessions: total,
		Counts:        counts,
	}, nil
}

func scanSkillOutcomes(rows interface {
	Next() bool
	Scan(...any) error
	Err() error
}) ([]types.SkillOutcome, error) {
	var out []types.SkillOutcome
	for rows.Next() {
		var o types.SkillOutcome
		if err := rows.Scan(
			&o.SessionID, &o.WorkspaceID, &o.IssueNumber,
			&o.SkillPath, &o.SkillHash, &o.Mode, &o.Outcome,
			&o.BlockedReason, &o.UserAction,
			&o.CostCents, &o.DurationMs, &o.TranscriptPath, &o.CapturedAt,
		); err != nil {
			return nil, err
		}
		out = append(out, o)
	}
	return out, rows.Err()
}

// nowUnixSkillOutcome returns the current Unix epoch for CapturedAt. Defined
// as a package-level var so tests can override it without time package tricks.
var nowUnixSkillOutcome = func() int64 { return time.Now().Unix() }
