package store

import (
	"database/sql"
	"errors"
	"fmt"
	"time"

	"prismconductor/internal/types"
)

// ListPools returns every pool row, oldest first.
func (s *Store) ListPools() ([]types.Pool, error) {
	if s == nil || s.DB == nil {
		return nil, errors.New("store unavailable")
	}
	rows, err := s.DB.Query(`
SELECT id, name, provider, endpoint, model, capacity, enabled, api_key, created_at
FROM pools
ORDER BY created_at ASC, id ASC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []types.Pool
	for rows.Next() {
		p, err := scanPool(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

// GetPool returns one pool by ID.
func (s *Store) GetPool(id string) (types.Pool, error) {
	if s == nil || s.DB == nil {
		return types.Pool{}, errors.New("store unavailable")
	}
	row := s.DB.QueryRow(`
SELECT id, name, provider, endpoint, model, capacity, enabled, api_key, created_at
FROM pools WHERE id = ?`, id)
	return scanPool(row)
}

// SavePool upserts a pool by ID.
func (s *Store) SavePool(p types.Pool) error {
	if s == nil || s.DB == nil {
		return errors.New("store unavailable")
	}
	if p.ID == "" {
		return errors.New("pool ID required")
	}
	if p.CreatedAt.IsZero() {
		p.CreatedAt = time.Now()
	}
	enabled := 0
	if p.Enabled {
		enabled = 1
	}
	_, err := s.DB.Exec(`
INSERT INTO pools (id, name, provider, endpoint, model, capacity, enabled, api_key, created_at)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
ON CONFLICT(id) DO UPDATE SET
    name = excluded.name,
    provider = excluded.provider,
    endpoint = excluded.endpoint,
    model = excluded.model,
    capacity = excluded.capacity,
    enabled = excluded.enabled,
    api_key = excluded.api_key`,
		p.ID, p.Name, string(p.Provider), p.Endpoint, p.Model,
		p.Capacity, enabled, p.APIKey, p.CreatedAt.Unix())
	return err
}

// DeletePool drops a pool row. Caller is responsible for the active-worker
// guard (see app.DeletePool).
func (s *Store) DeletePool(id string) error {
	if s == nil || s.DB == nil {
		return errors.New("store unavailable")
	}
	_, err := s.DB.Exec(`DELETE FROM pools WHERE id = ?`, id)
	return err
}

// PoolsCount reports the row count. Used at startup to gate one-shot
// migration of the legacy worker_pool_capacity setting into a Claude pool.
func (s *Store) PoolsCount() (int, error) {
	if s == nil || s.DB == nil {
		return 0, errors.New("store unavailable")
	}
	var n int
	if err := s.DB.QueryRow(`SELECT COUNT(*) FROM pools`).Scan(&n); err != nil {
		return 0, err
	}
	return n, nil
}

type rowScanner interface {
	Scan(dest ...any) error
}

func scanPool(r rowScanner) (types.Pool, error) {
	var p types.Pool
	var provider string
	var enabled int
	var createdAt int64
	if err := r.Scan(&p.ID, &p.Name, &provider, &p.Endpoint, &p.Model,
		&p.Capacity, &enabled, &p.APIKey, &createdAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return types.Pool{}, fmt.Errorf("pool not found")
		}
		return types.Pool{}, err
	}
	p.Provider = types.Provider(provider)
	p.Enabled = enabled != 0
	p.CreatedAt = time.Unix(createdAt, 0)
	return p, nil
}
