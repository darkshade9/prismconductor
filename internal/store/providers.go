package store

import (
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"prismconductor/internal/types"
)

// ErrProviderNotFound is returned when a provider row does not exist.
var ErrProviderNotFound = errors.New("provider not found")

// ErrProviderReferenced is returned by DeleteProviderEntity when one or more
// pools still reference the provider (q2: block deletion when referenced).
type ErrProviderReferenced struct {
	ProviderID string
	PoolIDs    []string
}

func (e ErrProviderReferenced) Error() string {
	return fmt.Sprintf("provider %q is referenced by %d pool(s): %s",
		e.ProviderID, len(e.PoolIDs), strings.Join(e.PoolIDs, ", "))
}

// ListProviderEntities returns every provider row, oldest first.
func (s *Store) ListProviderEntities() ([]types.ProviderEntity, error) {
	if s == nil || s.DB == nil {
		return nil, errors.New("store unavailable")
	}
	rows, err := s.DB.Query(
		`SELECT id, name, kind, endpoint, api_key, created_at FROM providers ORDER BY created_at ASC, id ASC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []types.ProviderEntity
	for rows.Next() {
		p, err := scanProvider(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

// GetProviderEntity returns one provider by ID.
func (s *Store) GetProviderEntity(id string) (types.ProviderEntity, error) {
	if s == nil || s.DB == nil {
		return types.ProviderEntity{}, errors.New("store unavailable")
	}
	row := s.DB.QueryRow(
		`SELECT id, name, kind, endpoint, api_key, created_at FROM providers WHERE id = ?`, id)
	p, err := scanProvider(row)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return types.ProviderEntity{}, ErrProviderNotFound
		}
		return types.ProviderEntity{}, err
	}
	return p, nil
}

// SaveProviderEntity upserts a provider row. Caller must set ID before calling.
func (s *Store) SaveProviderEntity(p types.ProviderEntity) error {
	if s == nil || s.DB == nil {
		return errors.New("store unavailable")
	}
	if p.ID == "" {
		return errors.New("provider ID required")
	}
	if p.CreatedAt.IsZero() {
		p.CreatedAt = time.Now()
	}
	_, err := s.DB.Exec(`
INSERT INTO providers (id, name, kind, endpoint, api_key, created_at)
VALUES (?, ?, ?, ?, ?, ?)
ON CONFLICT(id) DO UPDATE SET
    name      = excluded.name,
    kind      = excluded.kind,
    endpoint  = excluded.endpoint,
    api_key   = excluded.api_key`,
		p.ID, p.Name, string(p.Kind), p.Endpoint, p.APIKey, p.CreatedAt.Unix())
	return err
}

// DeleteProviderEntity removes a provider row. Returns ErrProviderReferenced
// when one or more pools still reference this provider (q2: block deletion).
func (s *Store) DeleteProviderEntity(id string) error {
	if s == nil || s.DB == nil {
		return errors.New("store unavailable")
	}
	rows, err := s.DB.Query(`SELECT id FROM pools WHERE provider_id = ?`, id)
	if err != nil {
		return err
	}
	var poolIDs []string
	for rows.Next() {
		var pid string
		if err := rows.Scan(&pid); err != nil {
			rows.Close()
			return err
		}
		poolIDs = append(poolIDs, pid)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}
	if len(poolIDs) > 0 {
		return ErrProviderReferenced{ProviderID: id, PoolIDs: poolIDs}
	}
	_, err = s.DB.Exec(`DELETE FROM providers WHERE id = ?`, id)
	return err
}

// MigratePoolsToProviders is a one-shot startup migration (issue #268, q3).
// It groups all pools that have no provider_id by (kind, endpoint, api_key),
// creates one ProviderEntity per unique group named "{kind}-default", and sets
// each pool's provider_id to point at its group. Returns (created, updated, err).
// Idempotent: pools already bearing a provider_id are skipped.
func (s *Store) MigratePoolsToProviders() (created, updated int, err error) {
	if s == nil || s.DB == nil {
		return 0, 0, errors.New("store unavailable")
	}

	// Fetch pools without a provider_id.
	rows, err := s.DB.Query(`SELECT id, provider, endpoint, api_key FROM pools WHERE provider_id IS NULL OR provider_id = ''`)
	if err != nil {
		return 0, 0, err
	}
	type poolRow struct {
		id       string
		provider string
		endpoint string
		apiKey   string
	}
	var unmigrated []poolRow
	for rows.Next() {
		var r poolRow
		if err := rows.Scan(&r.id, &r.provider, &r.endpoint, &r.apiKey); err != nil {
			rows.Close()
			return 0, 0, err
		}
		unmigrated = append(unmigrated, r)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return 0, 0, err
	}
	if len(unmigrated) == 0 {
		return 0, 0, nil
	}

	type groupKey struct{ kind, endpoint, apiKey string }
	groups := make(map[groupKey]string) // key → provider entity ID

	tx, err := s.DB.Begin()
	if err != nil {
		return 0, 0, err
	}
	defer tx.Rollback() //nolint:errcheck

	now := time.Now().Unix()
	for _, r := range unmigrated {
		k := groupKey{kind: strings.ToLower(r.provider), endpoint: r.endpoint, apiKey: r.apiKey}
		provID, seen := groups[k]
		if !seen {
			// Determine a unique name. Count existing providers with this kind prefix.
			var count int
			_ = tx.QueryRow(
				`SELECT COUNT(*) FROM providers WHERE name LIKE ?`,
				k.kind+"-%",
			).Scan(&count)
			name := fmt.Sprintf("%s-default", k.kind)
			if count > 0 {
				name = fmt.Sprintf("%s-default-%d", k.kind, count+1)
			}
			provID = uuid.New().String()
			if _, err := tx.Exec(
				`INSERT INTO providers (id, name, kind, endpoint, api_key, created_at) VALUES (?, ?, ?, ?, ?, ?)`,
				provID, name, k.kind, k.endpoint, k.apiKey, now,
			); err != nil {
				return 0, 0, err
			}
			groups[k] = provID
			created++
		}
		if _, err := tx.Exec(`UPDATE pools SET provider_id = ? WHERE id = ?`, provID, r.id); err != nil {
			return 0, 0, err
		}
		updated++
	}

	if err := tx.Commit(); err != nil {
		return 0, 0, err
	}
	return created, updated, nil
}

type providerScanner interface {
	Scan(dest ...any) error
}

func scanProvider(r providerScanner) (types.ProviderEntity, error) {
	var p types.ProviderEntity
	var kind string
	var createdAt int64
	if err := r.Scan(&p.ID, &p.Name, &kind, &p.Endpoint, &p.APIKey, &createdAt); err != nil {
		return types.ProviderEntity{}, err
	}
	p.Kind = types.Provider(kind)
	p.CreatedAt = time.Unix(createdAt, 0)
	return p, nil
}
