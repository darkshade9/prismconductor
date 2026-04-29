// Package workerpool tracks heterogeneous worker fleets (PRISMCONDUCTOR_PLAN.md
// §6.6, issue #27, role-tagged in #39).
//
// Each Pool is a single-fleet capacity counter. The Registry holds many of
// them keyed by ID and routes work by role. Eligibility is checked through a
// `func(types.Provider) bool` closure injected at construction — the package
// never references a specific Provider constant, so the orchestrator's routing
// layer stays provider-agnostic.
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
// provider-spawnability predicate. Acquire methods are role-keyed (#39); the
// registry never falls back across roles.
type Registry struct {
	mu       sync.RWMutex
	pools    map[string]*Pool
	meta     map[string]types.Pool
	order    []string
	canSpawn func(types.Provider) bool

	nextPlanIdx int
	nextWorkIdx int
}

// NewRegistry takes a spawnability predicate. The predicate returns true when
// a given provider's driver can produce a working argv today; the registry
// uses it as the single seam for provider-eligibility checks. role=plan and
// role=work pools are still listed when the provider isn't spawn-capable, but
// AcquireForPlan / AcquireForWork skip them so the orchestrator never reserves
// a slot it can't use.
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
// metadata (name, model, capacity, enabled, role). Pools that disappeared are
// dropped.
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
}

// AcquireForPlan reserves a plan slot. Selection order:
//  1. ws.SkillProfile.PreferredPlanPoolID, if set + enabled + role=plan +
//     spawn-capable provider + free.
//  2. Round-robin among enabled role=plan pools whose providers can spawn,
//     starting at the registry's rolling index.
//  3. ("", false). NO fallback to role=work — the orchestrator's auto-pull
//     surfaces this as a paused-state, not a silent retry on the wrong role.
func (r *Registry) AcquireForPlan(ws types.Workspace) (string, bool) {
	if id, ok := r.tryPreferred(ws.SkillProfile.PreferredPlanPoolID, types.RolePlan); ok {
		return id, true
	}
	return r.acquireRoundRobin(types.RolePlan)
}

// AcquireForWork mirrors AcquireForPlan for execute spawns. NO fallback to
// plan pools.
func (r *Registry) AcquireForWork(ws types.Workspace) (string, bool) {
	if id, ok := r.tryPreferred(ws.SkillProfile.PreferredWorkPoolID, types.RoleWork); ok {
		return id, true
	}
	return r.acquireRoundRobin(types.RoleWork)
}

// OrchestratorPool returns the single enabled role=orchestrator pool, or
// (Pool{}, false). Capacity isn't relevant — orchestrator runs are per-call
// HTTP and don't reserve a slot.
func (r *Registry) OrchestratorPool() (types.Pool, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	for _, id := range r.order {
		meta, ok := r.meta[id]
		if !ok {
			continue
		}
		if meta.Role != types.RoleOrchestrator || !meta.Enabled {
			continue
		}
		return meta, true
	}
	return types.Pool{}, false
}

func (r *Registry) tryPreferred(poolID string, role types.Role) (string, bool) {
	if poolID == "" {
		return "", false
	}
	r.mu.RLock()
	meta, hasMeta := r.meta[poolID]
	p := r.pools[poolID]
	r.mu.RUnlock()
	if !hasMeta || p == nil {
		return "", false
	}
	if meta.Role != role || !meta.Enabled || meta.Capacity <= 0 {
		return "", false
	}
	if !r.canSpawn(meta.Provider) {
		return "", false
	}
	if p.TryAcquire() {
		return poolID, true
	}
	return "", false
}

func (r *Registry) acquireRoundRobin(role types.Role) (string, bool) {
	r.mu.Lock()
	ids := make([]string, 0, len(r.order))
	for _, id := range r.order {
		meta, ok := r.meta[id]
		if !ok {
			continue
		}
		if meta.Role != role || !meta.Enabled || meta.Capacity <= 0 {
			continue
		}
		if !r.canSpawn(meta.Provider) {
			continue
		}
		ids = append(ids, id)
	}
	if len(ids) == 0 {
		r.mu.Unlock()
		return "", false
	}
	start := 0
	switch role {
	case types.RolePlan:
		start = r.nextPlanIdx % len(ids)
	case types.RoleWork:
		start = r.nextWorkIdx % len(ids)
	}
	r.mu.Unlock()

	for off := 0; off < len(ids); off++ {
		idx := (start + off) % len(ids)
		id := ids[idx]
		r.mu.RLock()
		p := r.pools[id]
		r.mu.RUnlock()
		if p == nil {
			continue
		}
		if p.TryAcquire() {
			r.mu.Lock()
			switch role {
			case types.RolePlan:
				r.nextPlanIdx = (idx + 1) % len(ids)
			case types.RoleWork:
				r.nextWorkIdx = (idx + 1) % len(ids)
			}
			r.mu.Unlock()
			return id, true
		}
	}
	return "", false
}

// ReleaseByPool returns a slot to the named pool. No-op if the pool was
// removed in the meantime.
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

// FreePlanSlots sums Free() across enabled role=plan pools whose providers can
// spawn.
func (r *Registry) FreePlanSlots() int { return r.freeByRole(types.RolePlan) }

// FreeWorkSlots sums Free() across enabled role=work pools whose providers can
// spawn.
func (r *Registry) FreeWorkSlots() int { return r.freeByRole(types.RoleWork) }

func (r *Registry) freeByRole(role types.Role) int {
	r.mu.RLock()
	defer r.mu.RUnlock()
	total := 0
	for _, id := range r.order {
		meta := r.meta[id]
		p := r.pools[id]
		if p == nil || !meta.Enabled || meta.Capacity <= 0 || meta.Role != role {
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
	sort.SliceStable(out, func(i, j int) bool {
		return out[i].Pool.CreatedAt.Before(out[j].Pool.CreatedAt)
	})
	return out
}
