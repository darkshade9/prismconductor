package store

import (
	"errors"
	"time"
)

// UpsertPoolDailyUsage adds sessions/tokens/cents to the day's aggregate row
// for poolID. date must be an ISO date string (YYYY-MM-DD UTC). Called from
// the session finalizer (#283).
func (s *Store) UpsertPoolDailyUsage(poolID, date string, sessions int, tokens int64, cents float64) error {
	if s == nil || s.DB == nil {
		return errors.New("store unavailable")
	}
	_, err := s.DB.Exec(`
INSERT INTO pool_daily_usage (pool_id, date, sessions, tokens, cents)
VALUES (?, ?, ?, ?, ?)
ON CONFLICT(pool_id, date) DO UPDATE SET
    sessions = pool_daily_usage.sessions + excluded.sessions,
    tokens   = pool_daily_usage.tokens   + excluded.tokens,
    cents    = pool_daily_usage.cents    + excluded.cents`,
		poolID, date, sessions, tokens, cents)
	return err
}

// PoolProjectionData returns spend and session counts for a pool from the
// sessions table (authoritative: covers pre-migration history). Returns
// (cents7d, cents30d float64, sessions30d, daysWithData int, err error).
func (s *Store) PoolProjectionData(poolID string) (cents7d, cents30d float64, sessions30d, daysWithData int, err error) {
	if s == nil || s.DB == nil {
		return 0, 0, 0, 0, errors.New("store unavailable")
	}
	now := time.Now().UTC()
	since7d := now.AddDate(0, 0, -7).Format(time.RFC3339)
	since30d := now.AddDate(0, 0, -30).Format(time.RFC3339)

	if e := s.DB.QueryRow(
		`SELECT COALESCE(SUM(estimated_cost_cents), 0) FROM sessions
		 WHERE json_extract(json, '$.pool_id') = ?
		   AND json_extract(json, '$.started_at') >= ?
		   AND state IN ('completed','failed','blocked')`,
		poolID, since7d,
	).Scan(&cents7d); e != nil {
		return 0, 0, 0, 0, e
	}

	rows, e := s.DB.Query(
		`SELECT estimated_cost_cents, json_extract(json, '$.started_at')
		 FROM sessions
		 WHERE json_extract(json, '$.pool_id') = ?
		   AND json_extract(json, '$.started_at') >= ?
		   AND state IN ('completed','failed','blocked')`,
		poolID, since30d,
	)
	if e != nil {
		return 0, 0, 0, 0, e
	}
	defer rows.Close()
	days := map[string]struct{}{}
	for rows.Next() {
		var c float64
		var startedAt string
		if e := rows.Scan(&c, &startedAt); e != nil {
			continue
		}
		cents30d += c
		sessions30d++
		if len(startedAt) >= 10 {
			days[startedAt[:10]] = struct{}{}
		}
	}
	if e := rows.Err(); e != nil {
		return 0, 0, 0, 0, e
	}
	daysWithData = len(days)
	return
}

// GlobalProjectionData returns aggregated spend and session counts across all
// pools from the sessions table.
func (s *Store) GlobalProjectionData() (cents7d, cents30d float64, sessions30d, daysWithData int, err error) {
	if s == nil || s.DB == nil {
		return 0, 0, 0, 0, errors.New("store unavailable")
	}
	now := time.Now().UTC()
	since7d := now.AddDate(0, 0, -7).Format(time.RFC3339)
	since30d := now.AddDate(0, 0, -30).Format(time.RFC3339)

	if e := s.DB.QueryRow(
		`SELECT COALESCE(SUM(estimated_cost_cents), 0) FROM sessions
		 WHERE json_extract(json, '$.started_at') >= ?
		   AND state IN ('completed','failed','blocked')`,
		since7d,
	).Scan(&cents7d); e != nil {
		return 0, 0, 0, 0, e
	}

	rows, e := s.DB.Query(
		`SELECT estimated_cost_cents, json_extract(json, '$.started_at')
		 FROM sessions
		 WHERE json_extract(json, '$.started_at') >= ?
		   AND state IN ('completed','failed','blocked')`,
		since30d,
	)
	if e != nil {
		return 0, 0, 0, 0, e
	}
	defer rows.Close()
	days := map[string]struct{}{}
	for rows.Next() {
		var c float64
		var startedAt string
		if e := rows.Scan(&c, &startedAt); e != nil {
			continue
		}
		cents30d += c
		sessions30d++
		if len(startedAt) >= 10 {
			days[startedAt[:10]] = struct{}{}
		}
	}
	if e := rows.Err(); e != nil {
		return 0, 0, 0, 0, e
	}
	daysWithData = len(days)
	return
}
