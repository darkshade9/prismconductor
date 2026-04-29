// Package workerpool tracks heterogeneous worker fleets (PRISMCONDUCTOR_PLAN.md
// §6.6, issue #27).
//
// Each Pool is a single-fleet capacity counter. The Registry holds many of
// them keyed by ID and routes work to the first eligible pool whose provider
// can spawn today. Eligibility is checked through a `func(types.Provider) bool`
// closure injected at construction — the package never references a specific
// Provider constant, so the orchestrator's routing layer stays
// provider-agnostic.
package workerpool

import (
	"sort"
	"sync"

	"prismconductor/internal/types"
)

type Pool struct {
	mu       sync.Mutex
	capacity int
	active   int
}

func New(capacity int) *Pool { return &Pool{capacity: capacity} }

func (p *Pool) Capacity() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.capacity
}

func (p *Pool) Active() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.active
}

func (p *Pool) Free() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.capacity - p.active
}

// SetCapacity changes capacity. Caller decides whether to publish EvtAgentCountChanged.
func (p *Pool) SetCapacity(n int) {
	p.mu.Lock()
	p.capacity = n
	p.mu.Unlock()
}

// TryAcquire reserves a slot if free.
func (p *Pool) TryAcquire() bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.active >= p.capacity {
		return false
	}
	p.active++
	return true
}

func (p *Pool) Release() {
	p.mu.Lock()
	if p.active > 0 {
		p.active--
	}
	p.mu.Unlock()
}

// PoolStatus is the per-pool snapshot surfaced to the UI.
type PoolStatus struct {
	Pool   types.Pool `json:"pool"`
	Active int        `json:"active"`
}

// Registry holds many *Pool rows keyed by pool ID and routes work via a
// provider-spawnability predicate.
type Registry struct {
	mu       sync.RWMutex
	pools    map[string]*Pool
	meta     map[string]types.Pool
	order    []string
	canSpawn func(types.Provider) bool
}

// NewRegistry takes a spawnability predicate. The predicate returns true when
// a given provider's driver can produce a working argv today; the registry
// uses it as the single seam for provider-eligibility checks.
func NewRegistry(canSpawn func(types.Provider) bool) *Registry {
	if canSpawn == nil {
		canSpawn = func(types.Provider) bool { return false }
	}
	return &Registry{
		pools:    map[string]*Pool{},
		meta:     map[string]types.Pool{},
		canSpawn: canSpawn,
	}
}

// Sync reconciles the registry with a fresh DB read. New pools get a
// fresh *Pool. Surviving entries keep their active counts but pick up updated
// metadata (name, model, capacity, enabled). Pools that disappeared are
// dropped (pending workers will Release into a stale pool — logged below).
func (r *Registry) Sync(rows []types.Pool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	keep := make(map[string]struct{}, len(rows))
	r.order = r.order[:0]
	for _, row := range rows {
		keep[row.ID] = struct{}{}
		r.order = append(r.order, row.ID)
		r.meta[row.ID] = row
		if p, ok := r.pools[row.ID]; ok {
			p.SetCapacity(row.Capacity)
			continue
		}
		r.pools[row.ID] = New(row.Capacity)
	}
	for id := range r.pools {
		if _, ok := keep[id]; !ok {
			delete(r.pools, id)
			delete(r.meta, id)
		}
	}
	// Stable order: keep the order rows came in.
}

// AcquireFor reserves a slot on the first eligible pool whose provider can
// spawn today. Iteration follows the most recent Sync's row order so behavior
// is deterministic. ws is currently unused — pinning is deferred per #27 q2 —
// but kept on the signature so future per-workspace pool pinning slots in
// without breaking callers.
func (r *Registry) AcquireFor(ws types.Workspace) (string, bool) {
	_ = ws
	r.mu.RLock()
	ids := append([]string(nil), r.order...)
	r.mu.RUnlock()
	for _, id := range ids {
		r.mu.RLock()
		meta, ok := r.meta[id]
		p := r.pools[id]
		r.mu.RUnlock()
		if !ok || p == nil {
			continue
		}
		if !meta.Enabled || meta.Capacity <= 0 {
			continue
		}
		if !r.canSpawn(meta.Provider) {
			continue
		}
		if p.TryAcquire() {
			return id, true
		}
	}
	return "", false
}

// ReleaseByPool returns a slot to the named pool. No-op if the pool was
// removed in the meantime — the worker is finishing under a stale config and
// the user moved on.
func (r *Registry) ReleaseByPool(poolID string) {
	r.mu.RLock()
	p := r.pools[poolID]
	r.mu.RUnlock()
	if p != nil {
		p.Release()
	}
}

// ActiveCount returns the in-flight worker count for one pool. Used by the
// app's DeletePool guard (rev4 q2: refuse delete on busy pools).
func (r *Registry) ActiveCount(poolID string) int {
	r.mu.RLock()
	p := r.pools[poolID]
	r.mu.RUnlock()
	if p == nil {
		return 0
	}
	return p.Active()
}

// FreeForSpawn sums Free() across enabled pools whose providers can spawn.
// Replaces the legacy "Pool.Free()" check on the singleton pool.
func (r *Registry) FreeForSpawn() int {
	r.mu.RLock()
	defer r.mu.RUnlock()
	total := 0
	for _, id := range r.order {
		meta := r.meta[id]
		p := r.pools[id]
		if p == nil || !meta.Enabled || meta.Capacity <= 0 {
			continue
		}
		if !r.canSpawn(meta.Provider) {
			continue
		}
		total += p.Free()
	}
	return total
}

// Snapshot returns per-pool status sorted by created_at (driven by Sync's
// order). Used by the UI's PoolsPanel.
func (r *Registry) Snapshot() []PoolStatus {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]PoolStatus, 0, len(r.order))
	for _, id := range r.order {
		meta, ok := r.meta[id]
		if !ok {
			continue
		}
		p := r.pools[id]
		active := 0
		if p != nil {
			active = p.Active()
		}
		out = append(out, PoolStatus{Pool: meta, Active: active})
	}
	// Defensive secondary sort to keep the UI stable across reorders that
	// share an id (shouldn't happen, but cheap insurance).
	sort.SliceStable(out, func(i, j int) bool {
		return out[i].Pool.CreatedAt.Before(out[j].Pool.CreatedAt)
	})
	return out
}
