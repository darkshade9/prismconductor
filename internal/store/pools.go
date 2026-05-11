package store

import (
	"database/sql"
	"errors"
	"fmt"
	"time"

	"prismconductor/internal/types"
)

// ErrInvalidRole is returned by SavePool when the pool's Role isn't one of
// types.RolePlan / RoleWork / RoleOrchestrator.
var ErrInvalidRole = errors.New("invalid pool role")

// ErrOrchestratorPoolExists is returned when SavePool would create a second
// enabled orchestrator pool. At-most-one is enforced in Go (issue #39 q4).
var ErrOrchestratorPoolExists = errors.New("an enabled orchestrator pool already exists; disable it first")

// ErrPoolExists is returned by CreatePool when a row with the same ID already exists.
var ErrPoolExists = errors.New("pool with that ID already exists")

// validatePool sets Role/CreatedAt defaults and validates required fields
// shared by CreatePool and SavePool.
func validatePool(p types.Pool) (types.Pool, error) {
	if p.ID == "" {
		return p, errors.New("pool ID required")
	}
	if p.Role == "" {
		p.Role = types.RoleWork
	}
	if !types.ValidRole(p.Role) {
		return p, fmt.Errorf("%w: %q", ErrInvalidRole, string(p.Role))
	}
	if p.CreatedAt.IsZero() {
		p.CreatedAt = time.Now()
	}
	return p, nil
}

// ListPools returns every pool row, oldest first.
func (s *Store) ListPools() ([]types.Pool, error) {
	if s == nil || s.DB == nil {
		return nil, errors.New("store unavailable")
	}
	rows, err := s.DB.Query(`
SELECT id, name, provider, endpoint, model, capacity, enabled, api_key, role, created_at, priority, temperature, max_turns, max_input_tokens, bash_timeout_ms, output_cap, scope, workspace_id, COALESCE(provider_id,'')
FROM pools
ORDER BY priority ASC, created_at ASC, id ASC`)
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

// ListPoolsByRole returns all pool rows tagged with the given role, oldest first.
func (s *Store) ListPoolsByRole(role types.Role) ([]types.Pool, error) {
	if s == nil || s.DB == nil {
		return nil, errors.New("store unavailable")
	}
	rows, err := s.DB.Query(`
SELECT id, name, provider, endpoint, model, capacity, enabled, api_key, role, created_at, priority, temperature, max_turns, max_input_tokens, bash_timeout_ms, output_cap, scope, workspace_id, COALESCE(provider_id,'')
FROM pools
WHERE role = ?
ORDER BY priority ASC, created_at ASC, id ASC`, string(role))
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
SELECT id, name, provider, endpoint, model, capacity, enabled, api_key, role, created_at, priority, temperature, max_turns, max_input_tokens, bash_timeout_ms, output_cap, scope, workspace_id, COALESCE(provider_id,'')
FROM pools WHERE id = ?`, id)
	return scanPool(row)
}

// SavePool upserts a pool by ID. Use CreatePool for new rows — it auto-assigns
// priority so new pools land at the end of the preference order.
func (s *Store) SavePool(p types.Pool) error {
	if s == nil || s.DB == nil {
		return errors.New("store unavailable")
	}
	var err error
	if p, err = validatePool(p); err != nil {
		return err
	}
	if p.Role == types.RoleOrchestrator && p.Enabled {
		// Refuse a second enabled orchestrator pool. Allowed when this row
		// is the same one being updated.
		existing, err := s.ListPoolsByRole(types.RoleOrchestrator)
		if err != nil {
			return err
		}
		for _, e := range existing {
			if e.ID == p.ID {
				continue
			}
			if e.Enabled {
				return ErrOrchestratorPoolExists
			}
		}
	}
	enabled := 0
	if p.Enabled {
		enabled = 1
	}
	var bashTimeoutMs *int64
	if p.BashTimeout != nil {
		ms := p.BashTimeout.Milliseconds()
		bashTimeoutMs = &ms
	}
	scope := string(p.Scope)
	if scope == "" {
		scope = string(types.PoolScopeShared)
	}
	_, err = s.DB.Exec(`
INSERT INTO pools (id, name, provider, endpoint, model, capacity, enabled, api_key, role, created_at, priority, temperature, max_turns, max_input_tokens, bash_timeout_ms, output_cap, scope, workspace_id, provider_id)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
ON CONFLICT(id) DO UPDATE SET
    name = excluded.name,
    provider = excluded.provider,
    endpoint = excluded.endpoint,
    model = excluded.model,
    capacity = excluded.capacity,
    enabled = excluded.enabled,
    api_key = excluded.api_key,
    role = excluded.role,
    priority = excluded.priority,
    temperature = excluded.temperature,
    max_turns = excluded.max_turns,
    max_input_tokens = excluded.max_input_tokens,
    bash_timeout_ms = excluded.bash_timeout_ms,
    output_cap = excluded.output_cap,
    scope = excluded.scope,
    workspace_id = excluded.workspace_id,
    provider_id = excluded.provider_id`,
		p.ID, p.Name, string(p.Provider), p.Endpoint, p.Model,
		p.Capacity, enabled, p.APIKey, string(p.Role), p.CreatedAt.Unix(), p.Priority, p.Temperature,
		p.MaxTurns, p.MaxInputTokens, bashTimeoutMs, p.OutputCap, scope, p.WorkspaceID, nullableString(p.ProviderID))
	return err
}

// CreatePool inserts a brand-new pool, auto-assigning priority to the end of
// the preference order for its role. Only enabled same-role rows contribute to
// the MAX; a new pool with no enabled peers lands at priority=0. Returns
// ErrPoolExists if a row with this ID already exists.
func (s *Store) CreatePool(p types.Pool) error {
	if s == nil || s.DB == nil {
		return errors.New("store unavailable")
	}
	var err error
	if p, err = validatePool(p); err != nil {
		return err
	}
	tx, err := s.DB.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback() //nolint:errcheck

	var count int
	if err := tx.QueryRow(`SELECT COUNT(*) FROM pools WHERE id = ?`, p.ID).Scan(&count); err != nil {
		return err
	}
	if count > 0 {
		return ErrPoolExists
	}

	if p.Role == types.RoleOrchestrator && p.Enabled {
		var orchCount int
		if err := tx.QueryRow(`SELECT COUNT(*) FROM pools WHERE role = 'orchestrator' AND enabled = 1`).Scan(&orchCount); err != nil {
			return err
		}
		if orchCount > 0 {
			return ErrOrchestratorPoolExists
		}
	}

	// Priority = MAX(priority)+1 over enabled same-role rows; COALESCE(-1)+1=0 when no peers.
	var maxPri int
	if err := tx.QueryRow(`SELECT COALESCE(MAX(priority), -1) FROM pools WHERE role = ? AND enabled = 1`, string(p.Role)).Scan(&maxPri); err != nil {
		return err
	}
	p.Priority = maxPri + 1

	enabled := 0
	if p.Enabled {
		enabled = 1
	}
	var bashTimeoutMs *int64
	if p.BashTimeout != nil {
		ms := p.BashTimeout.Milliseconds()
		bashTimeoutMs = &ms
	}
	scope := string(p.Scope)
	if scope == "" {
		scope = string(types.PoolScopeShared)
	}
	_, err = tx.Exec(`
INSERT INTO pools (id, name, provider, endpoint, model, capacity, enabled, api_key, role, created_at, priority, temperature, max_turns, max_input_tokens, bash_timeout_ms, output_cap, scope, workspace_id, provider_id)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		p.ID, p.Name, string(p.Provider), p.Endpoint, p.Model,
		p.Capacity, enabled, p.APIKey, string(p.Role), p.CreatedAt.Unix(), p.Priority, p.Temperature,
		p.MaxTurns, p.MaxInputTokens, bashTimeoutMs, p.OutputCap, scope, p.WorkspaceID, nullableString(p.ProviderID))
	if err != nil {
		return err
	}
	return tx.Commit()
}

// ReconcilePoolPriorities walks each role and reassigns sequential priorities
// to any rows tied at the same priority value. Display order (priority ASC,
// created_at ASC, id ASC — matching ListPools) is preserved. Idempotent:
// no-op when every role already has strictly increasing priorities.
// Returns the number of rows mutated.
func (s *Store) ReconcilePoolPriorities() (int, error) {
	if s == nil || s.DB == nil {
		return 0, errors.New("store unavailable")
	}
	roles := []types.Role{types.RolePlan, types.RoleWork, types.RoleOrchestrator, types.RoleArchitect}
	total := 0
	for _, role := range roles {
		rows, err := s.DB.Query(`
SELECT id, priority FROM pools
WHERE role = ?
ORDER BY priority ASC, created_at ASC, id ASC`, string(role))
		if err != nil {
			return total, err
		}
		type poolRow struct {
			id       string
			priority int
		}
		var poolRows []poolRow
		for rows.Next() {
			var r poolRow
			if err := rows.Scan(&r.id, &r.priority); err != nil {
				rows.Close()
				return total, err
			}
			poolRows = append(poolRows, r)
		}
		rows.Close()
		if err := rows.Err(); err != nil {
			return total, err
		}

		hasTies := false
		for i := 1; i < len(poolRows); i++ {
			if poolRows[i].priority == poolRows[i-1].priority {
				hasTies = true
				break
			}
		}
		if !hasTies {
			continue
		}

		tx, err := s.DB.Begin()
		if err != nil {
			return total, err
		}
		for i, r := range poolRows {
			if _, err := tx.Exec(`UPDATE pools SET priority = ? WHERE id = ?`, i, r.id); err != nil {
				_ = tx.Rollback()
				return total, err
			}
			total++
		}
		if err := tx.Commit(); err != nil {
			return total, err
		}
	}
	return total, nil
}

// RebindWorkspacePools sets scope='shared' and workspace_id='' for every pool
// bound to workspaceID. Called on workspace deletion so capacity isn't orphaned
// (issue #109, q1: silently rebind on delete).
func (s *Store) RebindWorkspacePools(workspaceID string) error {
	if s == nil || s.DB == nil {
		return errors.New("store unavailable")
	}
	_, err := s.DB.Exec(
		`UPDATE pools SET scope = 'shared', workspace_id = '' WHERE workspace_id = ?`,
		workspaceID)
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
	var role string
	var enabled int
	var createdAt int64
	var temperature sql.NullFloat64
	var maxTurns, maxInputTokens, bashTimeoutMs, outputCap sql.NullInt64
	var scope, workspaceID, providerID string
	if err := r.Scan(&p.ID, &p.Name, &provider, &p.Endpoint, &p.Model,
		&p.Capacity, &enabled, &p.APIKey, &role, &createdAt, &p.Priority, &temperature,
		&maxTurns, &maxInputTokens, &bashTimeoutMs, &outputCap, &scope, &workspaceID, &providerID); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return types.Pool{}, fmt.Errorf("pool not found")
		}
		return types.Pool{}, err
	}
	p.ProviderID = providerID
	p.Provider = types.Provider(provider)
	p.Role = types.Role(role)
	if p.Role == "" {
		p.Role = types.RoleWork
	}
	p.Enabled = enabled != 0
	p.CreatedAt = time.Unix(createdAt, 0)
	if temperature.Valid {
		p.Temperature = &temperature.Float64
	}
	if maxTurns.Valid {
		v := int(maxTurns.Int64)
		p.MaxTurns = &v
	}
	if maxInputTokens.Valid {
		v := int(maxInputTokens.Int64)
		p.MaxInputTokens = &v
	}
	if bashTimeoutMs.Valid {
		d := time.Duration(bashTimeoutMs.Int64) * time.Millisecond
		p.BashTimeout = &d
	}
	if outputCap.Valid {
		v := int(outputCap.Int64)
		p.OutputCap = &v
	}
	if scope == "" {
		scope = string(types.PoolScopeShared)
	}
	p.Scope = types.PoolScope(scope)
	p.WorkspaceID = workspaceID
	return p, nil
}

