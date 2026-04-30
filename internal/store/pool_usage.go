package store

import (
	"errors"
	"time"

	"prismconductor/internal/types"
)

// UpsertPoolUsage writes a rate-limit snapshot. The primary key is
// (pool_id, window); an existing row for the same key is replaced.
func (s *Store) UpsertPoolUsage(u types.PoolUsage) error {
	if s == nil || s.DB == nil {
		return errors.New("store unavailable")
	}
	_, err := s.DB.Exec(`
INSERT INTO pool_usage (pool_id, window, limit_value, used, resets_at, captured_at)
VALUES (?, ?, ?, ?, ?, ?)
ON CONFLICT(pool_id, window) DO UPDATE SET
    limit_value = excluded.limit_value,
    used        = excluded.used,
    resets_at   = excluded.resets_at,
    captured_at = excluded.captured_at`,
		u.PoolID, u.Window, u.LimitValue, u.Used,
		u.ResetsAt.Unix(), u.CapturedAt.Unix())
	return err
}

// ListPoolUsage returns the latest snapshot for every (pool, window) pair,
// joined with the pools table to fill in PoolName and Provider.
// Rows without a matching pool (orphaned after a pool deletion) are skipped.
func (s *Store) ListPoolUsage() ([]types.PoolUsage, error) {
	if s == nil || s.DB == nil {
		return nil, errors.New("store unavailable")
	}
	rows, err := s.DB.Query(`
SELECT pu.pool_id, p.name, p.provider, pu.window,
       pu.limit_value, pu.used, pu.resets_at, pu.captured_at
FROM pool_usage pu
JOIN pools p ON p.id = pu.pool_id
ORDER BY p.created_at ASC, pu.window ASC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []types.PoolUsage
	for rows.Next() {
		var u types.PoolUsage
		var resetsAt, capturedAt int64
		if err := rows.Scan(&u.PoolID, &u.PoolName, &u.Provider, &u.Window,
			&u.LimitValue, &u.Used, &resetsAt, &capturedAt); err != nil {
			return nil, err
		}
		u.ResetsAt = time.Unix(resetsAt, 0)
		u.CapturedAt = time.Unix(capturedAt, 0)
		out = append(out, u)
	}
	return out, rows.Err()
}
