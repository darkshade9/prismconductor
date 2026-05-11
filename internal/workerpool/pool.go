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

// AcquireForce increments the active counter unconditionally. Used by
// Registry.AcquireSpecific during startup rehydration to mark a slot that was
// already occupied before shutdown.
func (p *Pool) AcquireForce() {
	p.mu.Lock()
	p.active++
	p.mu.Unlock()
}

// ResetActive zeros the in-memory active counter. Used by the Settings →
// "Reset pool counters" button to recover from drift caused by orphaned
// sessions (e.g. conductor killed mid-session before tailAndParse could
// publish WorkerSlotFreed). Does not affect any actual running worker —
// only the counter accounting.
func (p *Pool) ResetActive() {
	p.mu.Lock()
	p.active = 0
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

	nextPlanIdx      int
	nextWorkIdx      int
	nextArchitectIdx int
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

// SetActive overwrites the in-memory active counter to n (clamped >= 0).
// Called by ReconcileActive to align the registry's view with DB truth.
func (p *Pool) SetActive(n int) {
	if n < 0 {
		n = 0
	}
	p.mu.Lock()
	p.active = n
	p.mu.Unlock()
}

// ReconcileActive aligns every pool's in-memory `active` counter with the
// authoritative DB count of currently-running sessions.
//
// Why this exists: the Registry's `active` counter is hand-maintained via
// paired TryAcquire/Release calls. Any code path that acquires a slot but
// errors before the matching Release runs leaks the slot in the counter
// forever — the user sees ghost slots (e.g. Planners 1/2 with zero plan
// sessions actually running). Manual "Reset counters" was the only fix.
//
// Now: callers (app.go startup, the bus subscriber on session-state and
// worker-slot-freed events) feed the registry the DB-derived counts via
// Store.CountRunningSessionsByPool, and the registry trusts that as truth.
// Drift becomes self-correcting: the counter is at most one terminal-event
// tick stale before reconcile pulls it back in line.
//
// Pools whose ID is not in the counts map are reset to 0 (no live
// sessions on them). Pools that no longer exist in the registry are
// silently ignored.
func (r *Registry) ReconcileActive(counts map[string]int) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	for id, p := range r.pools {
		if p == nil {
			continue
		}
		p.SetActive(counts[id])
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
//  2. Workspace-specific pools bound to ws.ID (issue #109), by priority.
//  3. Round-robin among enabled shared role=plan pools.
//  4. ("", false). NO fallback to role=work — the orchestrator's auto-pull
//     surfaces this as a paused-state, not a silent retry on the wrong role.
func (r *Registry) AcquireForPlan(ws types.Workspace) (string, bool) {
	if id, ok := r.tryPreferred(ws.SkillProfile.PreferredPlanPoolID, types.RolePlan); ok {
		return id, true
	}
	if ws.ID != "" {
		if id, ok := r.acquireWorkspaceScoped(ws.ID, types.RolePlan); ok {
			return id, true
		}
	}
	return r.acquireRoundRobin(types.RolePlan)
}

// AcquireForWork mirrors AcquireForPlan for execute spawns. NO fallback to
// plan pools.
func (r *Registry) AcquireForWork(ws types.Workspace) (string, bool) {
	if id, ok := r.tryPreferred(ws.SkillProfile.PreferredWorkPoolID, types.RoleWork); ok {
		return id, true
	}
	if ws.ID != "" {
		if id, ok := r.acquireWorkspaceScoped(ws.ID, types.RoleWork); ok {
			return id, true
		}
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

// acquireWorkspaceScoped tries to reserve a slot from pools whose scope=workspace
// and workspace_id matches the given workspaceID. Pools are tried in priority
// order (lower value = preferred). No round-robin: workspace-dedicated pools are
// typically few and the caller falls through to shared pools on failure.
func (r *Registry) acquireWorkspaceScoped(workspaceID string, role types.Role) (string, bool) {
	r.mu.Lock()
	ids := make([]string, 0)
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
		if meta.Scope != types.PoolScopeWorkspace || meta.WorkspaceID != workspaceID {
			continue
		}
		ids = append(ids, id)
	}
	sort.SliceStable(ids, func(i, j int) bool {
		return r.meta[ids[i]].Priority < r.meta[ids[j]].Priority
	})
	r.mu.Unlock()
	for _, id := range ids {
		r.mu.RLock()
		p := r.pools[id]
		r.mu.RUnlock()
		if p == nil {
			continue
		}
		if p.TryAcquire() {
			return id, true
		}
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
		// Skip workspace-specific pools; those are handled by acquireWorkspaceScoped.
		if meta.Scope == types.PoolScopeWorkspace {
			continue
		}
		ids = append(ids, id)
	}
	if len(ids) == 0 {
		r.mu.Unlock()
		return "", false
	}
	// Sort by priority ASC (lower = preferred). Pools with equal priority keep
	// their insertion order (SliceStable). This implements #40 preference ordering.
	sort.SliceStable(ids, func(i, j int) bool {
		return r.meta[ids[i]].Priority < r.meta[ids[j]].Priority
	})
	// Apply round-robin within the lowest-priority-value group so equal-priority
	// pools share load. Higher-priority groups (lower number) are always tried
	// first before falling back to lower-priority groups.
	if len(ids) > 0 {
		topPriority := r.meta[ids[0]].Priority
		prefixLen := 0
		for _, id := range ids {
			if r.meta[id].Priority != topPriority {
				break
			}
			prefixLen++
		}
		start := 0
		switch role {
		case types.RolePlan:
			start = r.nextPlanIdx % prefixLen
			r.nextPlanIdx = (r.nextPlanIdx + 1) % prefixLen
		case types.RoleWork:
			start = r.nextWorkIdx % prefixLen
			r.nextWorkIdx = (r.nextWorkIdx + 1) % prefixLen
		case types.RoleArchitect:
			start = r.nextArchitectIdx % prefixLen
			r.nextArchitectIdx = (r.nextArchitectIdx + 1) % prefixLen
		}
		// Rotate the preferred group to start at `start`, leave rest unchanged.
		preferred := append(ids[start:prefixLen:prefixLen], ids[:start]...)
		ids = append(preferred, ids[prefixLen:]...)
	}
	r.mu.Unlock()

	for _, id := range ids {
		r.mu.RLock()
		p := r.pools[id]
		r.mu.RUnlock()
		if p == nil {
			continue
		}
		if p.TryAcquire() {
			return id, true
		}
	}
	return "", false
}

// ResetAllActive zeros the in-memory active counter on every registered pool.
// Used by the Settings → "Reset pool counters" admin action to recover from
// drift. Returns the number of pools touched.
func (r *Registry) ResetAllActive() int {
	r.mu.RLock()
	pools := make([]*Pool, 0, len(r.pools))
	for _, p := range r.pools {
		pools = append(pools, p)
	}
	r.mu.RUnlock()
	for _, p := range pools {
		p.ResetActive()
	}
	return len(pools)
}

// AcquireSpecific increments the active counter for the named pool regardless
// of current free capacity. Returns (true, nil) when the pool exists and was
// incremented; (false, nil) when the pool ID is not in the registry (unknown
// or removed since shutdown). Used by session.Reattach to rehydrate live slots
// so the UI worker counter reflects reality after a restart.
func (r *Registry) AcquireSpecific(poolID string) (bool, error) {
	r.mu.RLock()
	p := r.pools[poolID]
	r.mu.RUnlock()
	if p == nil {
		return false, nil
	}
	p.AcquireForce()
	return true, nil
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

// FreeArchitectSlots sums Free() across enabled role=architect pools.
func (r *Registry) FreeArchitectSlots() int { return r.freeByRole(types.RoleArchitect) }

// AcquireForArchitect reserves a slot on a role=architect pool. Returns
// ("", false) when no architect pool is configured or all slots are busy.
// NO fallback to other roles.
func (r *Registry) AcquireForArchitect() (string, bool) {
	return r.acquireRoundRobin(types.RoleArchitect)
}

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
	// Sort by priority ASC, then created_at for tie-breaking (matches pool
	// selection order so the UI reflects actual acquisition preference).
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Pool.Priority != out[j].Pool.Priority {
			return out[i].Pool.Priority < out[j].Pool.Priority
		}
		return out[i].Pool.CreatedAt.Before(out[j].Pool.CreatedAt)
	})
	return out
}
