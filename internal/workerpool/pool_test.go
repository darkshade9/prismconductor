package workerpool

import (
	"testing"

	"prismconductor/internal/types"
)

func TestPoolTryAcquireRelease(t *testing.T) {
	p := New(2)
	if !p.TryAcquire() || !p.TryAcquire() {
		t.Fatal("expected first two acquires to succeed")
	}
	if p.TryAcquire() {
		t.Fatal("expected third acquire to fail when capacity is full")
	}
	if p.Free() != 0 {
		t.Fatalf("Free=%d, want 0", p.Free())
	}
	p.Release()
	if p.Free() != 1 {
		t.Fatalf("Free after release=%d, want 1", p.Free())
	}
}

func TestRegistrySyncTracksMembership(t *testing.T) {
	r := NewRegistry(func(types.Provider) bool { return true })
	r.Sync([]types.Pool{
		{ID: "a", Provider: types.ProviderClaude, Capacity: 1, Enabled: true, Role: types.RoleWork},
		{ID: "b", Provider: types.ProviderClaude, Capacity: 2, Enabled: true, Role: types.RoleWork},
	})
	if got := r.FreeWorkSlots(); got != 3 {
		t.Errorf("FreeWorkSlots after Sync = %d, want 3", got)
	}

	if _, ok := r.AcquireForWork(types.Workspace{}); !ok {
		t.Fatal("AcquireForWork failed unexpectedly")
	}
	r.Sync([]types.Pool{
		{ID: "b", Provider: types.ProviderClaude, Capacity: 2, Enabled: true, Role: types.RoleWork},
	})
	if got := r.FreeWorkSlots(); got != 2 {
		t.Errorf("FreeWorkSlots after dropping a = %d, want 2", got)
	}
}

func TestRegistryAcquireForWorkRespectsCanSpawn(t *testing.T) {
	r := NewRegistry(func(p types.Provider) bool { return p == types.ProviderClaude })
	r.Sync([]types.Pool{
		{ID: "openai", Provider: types.ProviderOpenAI, Capacity: 5, Enabled: true, Role: types.RoleWork},
		{ID: "claude", Provider: types.ProviderClaude, Capacity: 1, Enabled: true, Role: types.RoleWork},
	})
	got, ok := r.AcquireForWork(types.Workspace{})
	if !ok {
		t.Fatal("AcquireForWork returned ok=false despite a Claude pool being eligible")
	}
	if got != "claude" {
		t.Fatalf("AcquireForWork picked %q, want claude (only spawn-capable pool)", got)
	}
}

func TestRegistryAcquireSkipsDisabledAndZeroCapacity(t *testing.T) {
	r := NewRegistry(func(types.Provider) bool { return true })
	r.Sync([]types.Pool{
		{ID: "off", Provider: types.ProviderClaude, Capacity: 5, Enabled: false, Role: types.RoleWork},
		{ID: "zero", Provider: types.ProviderClaude, Capacity: 0, Enabled: true, Role: types.RoleWork},
		{ID: "ok", Provider: types.ProviderClaude, Capacity: 1, Enabled: true, Role: types.RoleWork},
	})
	got, ok := r.AcquireForWork(types.Workspace{})
	if !ok || got != "ok" {
		t.Fatalf("AcquireForWork returned (%q, %v), want (ok, true)", got, ok)
	}
}

func TestRegistryActiveCount(t *testing.T) {
	r := NewRegistry(func(types.Provider) bool { return true })
	r.Sync([]types.Pool{
		{ID: "a", Provider: types.ProviderClaude, Capacity: 2, Enabled: true, Role: types.RoleWork},
	})
	if r.ActiveCount("a") != 0 {
		t.Fatalf("initial ActiveCount = %d, want 0", r.ActiveCount("a"))
	}
	if _, ok := r.AcquireForWork(types.Workspace{}); !ok {
		t.Fatal("AcquireForWork failed")
	}
	if r.ActiveCount("a") != 1 {
		t.Fatalf("ActiveCount post-acquire = %d, want 1", r.ActiveCount("a"))
	}
	r.ReleaseByPool("a")
	if r.ActiveCount("a") != 0 {
		t.Fatalf("ActiveCount post-release = %d, want 0", r.ActiveCount("a"))
	}
}

// TestAcquireForPlan_NoFallbackWhenAbsent locks in the rev2 contract: when
// no role=plan pool is enabled, AcquireForPlan returns ("", false) — no
// silent spillover onto role=work pools.
func TestAcquireForPlan_NoFallbackWhenAbsent(t *testing.T) {
	r := NewRegistry(func(types.Provider) bool { return true })
	r.Sync([]types.Pool{
		{ID: "work-only", Provider: types.ProviderClaude, Capacity: 5, Enabled: true, Role: types.RoleWork},
	})
	if id, ok := r.AcquireForPlan(types.Workspace{}); ok {
		t.Fatalf("AcquireForPlan acquired %q despite no plan pool — must NOT fall back to work", id)
	}
}

func TestAcquireForWork_NoFallbackWhenAbsent(t *testing.T) {
	r := NewRegistry(func(types.Provider) bool { return true })
	r.Sync([]types.Pool{
		{ID: "plan-only", Provider: types.ProviderClaude, Capacity: 5, Enabled: true, Role: types.RolePlan},
	})
	if id, ok := r.AcquireForWork(types.Workspace{}); ok {
		t.Fatalf("AcquireForWork acquired %q despite no work pool — must NOT fall back to plan", id)
	}
}

func TestOrchestratorPool_PicksEnabled(t *testing.T) {
	r := NewRegistry(func(types.Provider) bool { return true })
	r.Sync([]types.Pool{
		{ID: "off", Provider: types.ProviderOllama, Capacity: 1, Enabled: false, Role: types.RoleOrchestrator},
		{ID: "on", Provider: types.ProviderLMStudio, Capacity: 1, Enabled: true, Role: types.RoleOrchestrator},
	})
	got, ok := r.OrchestratorPool()
	if !ok {
		t.Fatal("OrchestratorPool returned ok=false despite an enabled row")
	}
	if got.ID != "on" {
		t.Fatalf("OrchestratorPool picked %q, want on", got.ID)
	}
}

func TestOrchestratorPool_NoneConfigured(t *testing.T) {
	r := NewRegistry(func(types.Provider) bool { return true })
	r.Sync([]types.Pool{
		{ID: "p", Provider: types.ProviderClaude, Capacity: 1, Enabled: true, Role: types.RolePlan},
	})
	if _, ok := r.OrchestratorPool(); ok {
		t.Fatal("OrchestratorPool returned ok=true with no orchestrator row")
	}
}

func TestRoundRobinPlanPools(t *testing.T) {
	r := NewRegistry(func(types.Provider) bool { return true })
	r.Sync([]types.Pool{
		{ID: "p1", Provider: types.ProviderClaude, Capacity: 1, Enabled: true, Role: types.RolePlan},
		{ID: "p2", Provider: types.ProviderClaude, Capacity: 1, Enabled: true, Role: types.RolePlan},
	})
	first, ok := r.AcquireForPlan(types.Workspace{})
	if !ok {
		t.Fatal("first AcquireForPlan failed")
	}
	r.ReleaseByPool(first)
	second, ok := r.AcquireForPlan(types.Workspace{})
	if !ok {
		t.Fatal("second AcquireForPlan failed")
	}
	if first == second {
		t.Fatalf("expected round-robin to alternate pools, got %q twice", first)
	}
}

// TestAcquireForPlan_NonClaudeProviderAcceptedByRegistry: when canSpawn
// reports true for non-Claude (e.g. once harness-v1 lands), the registry
// happily acquires it. The provider gating is the canSpawn closure's job, not
// a role-by-provider filter. (Refine: roles are not provider-gated.)
func TestAcquireForPlan_NonClaudeProviderAcceptedByRegistry(t *testing.T) {
	r := NewRegistry(func(types.Provider) bool { return true })
	r.Sync([]types.Pool{
		{ID: "openai-plan", Provider: types.ProviderOpenAI, Capacity: 1, Enabled: true, Role: types.RolePlan},
	})
	id, ok := r.AcquireForPlan(types.Workspace{})
	if !ok || id != "openai-plan" {
		t.Fatalf("AcquireForPlan = (%q, %v), want (openai-plan, true)", id, ok)
	}
}

func TestAcquireForPlan_PrefersWorkspacePin(t *testing.T) {
	r := NewRegistry(func(types.Provider) bool { return true })
	r.Sync([]types.Pool{
		{ID: "p1", Provider: types.ProviderClaude, Capacity: 1, Enabled: true, Role: types.RolePlan},
		{ID: "p2", Provider: types.ProviderClaude, Capacity: 1, Enabled: true, Role: types.RolePlan},
	})
	ws := types.Workspace{SkillProfile: types.SkillProfile{PreferredPlanPoolID: "p2"}}
	id, ok := r.AcquireForPlan(ws)
	if !ok || id != "p2" {
		t.Fatalf("AcquireForPlan with pin = (%q, %v), want (p2, true)", id, ok)
	}
}

// TestPriorityOrderingPlanPools verifies that a pool with lower Priority value
// is acquired before a pool with higher Priority value (issue #40).
func TestPriorityOrderingPlanPools(t *testing.T) {
	r := NewRegistry(func(types.Provider) bool { return true })
	// p2 has priority 0 (preferred), p1 has priority 1 (fallback).
	r.Sync([]types.Pool{
		{ID: "p1", Provider: types.ProviderClaude, Capacity: 2, Enabled: true, Role: types.RolePlan, Priority: 1},
		{ID: "p2", Provider: types.ProviderClaude, Capacity: 2, Enabled: true, Role: types.RolePlan, Priority: 0},
	})
	id, ok := r.AcquireForPlan(types.Workspace{})
	if !ok || id != "p2" {
		t.Fatalf("AcquireForPlan with priority ordering = (%q, %v), want (p2, true)", id, ok)
	}
}

// TestPriorityFallbackWhenPreferredFull verifies fallback to lower-preference
// pool when the preferred pool is at capacity (issue #40).
func TestPriorityFallbackWhenPreferredFull(t *testing.T) {
	r := NewRegistry(func(types.Provider) bool { return true })
	r.Sync([]types.Pool{
		{ID: "preferred", Provider: types.ProviderClaude, Capacity: 1, Enabled: true, Role: types.RolePlan, Priority: 0},
		{ID: "fallback", Provider: types.ProviderClaude, Capacity: 1, Enabled: true, Role: types.RolePlan, Priority: 1},
	})
	// Fill the preferred pool.
	first, ok := r.AcquireForPlan(types.Workspace{})
	if !ok || first != "preferred" {
		t.Fatalf("first acquire = (%q, %v), want (preferred, true)", first, ok)
	}
	// Second acquire should fall back to the fallback pool.
	second, ok := r.AcquireForPlan(types.Workspace{})
	if !ok || second != "fallback" {
		t.Fatalf("second acquire = (%q, %v), want (fallback, true)", second, ok)
	}
}

// TestReconcileActiveSelfHealsLeakedSlot verifies the self-heal contract:
// a pool whose in-memory `active` counter has drifted ahead of DB truth
// (e.g. a previous spawn acquired but errored before releasing) gets
// pulled back into line by ReconcileActive(counts). This is the
// permanent fix for the "Planners 1/2 with zero sessions running" ghost
// — the registry trusts the DB count, not its own ledger.
func TestReconcileActiveSelfHealsLeakedSlot(t *testing.T) {
	r := NewRegistry(func(types.Provider) bool { return true })
	r.Sync([]types.Pool{
		{ID: "p1", Provider: types.ProviderClaude, Capacity: 2, Enabled: true, Role: types.RolePlan},
	})
	// Simulate a leak: acquire twice without releasing. Registry now thinks
	// active=2 (full) but DB has zero running sessions on this pool.
	if _, ok := r.AcquireForPlan(types.Workspace{}); !ok {
		t.Fatal("first acquire should succeed")
	}
	if _, ok := r.AcquireForPlan(types.Workspace{}); !ok {
		t.Fatal("second acquire should succeed")
	}
	if _, ok := r.AcquireForPlan(types.Workspace{}); ok {
		t.Fatal("third acquire should fail (pool full per leaked counter)")
	}

	// DB truth says zero sessions are running on p1. Reconcile.
	r.ReconcileActive(map[string]int{})

	// Now a fresh acquire should succeed because the leak self-healed.
	if _, ok := r.AcquireForPlan(types.Workspace{}); !ok {
		t.Fatal("post-reconcile acquire should succeed (leak should have self-healed)")
	}
	r.mu.RLock()
	got := r.pools["p1"].Active()
	r.mu.RUnlock()
	if got != 1 {
		t.Errorf("post-reconcile-then-acquire active = %d, want 1", got)
	}
}

// TestReconcileActiveAlignsWithDBCount confirms ReconcileActive sets the
// counter to whatever the DB-derived map says, even if it's nonzero.
func TestReconcileActiveAlignsWithDBCount(t *testing.T) {
	r := NewRegistry(func(types.Provider) bool { return true })
	r.Sync([]types.Pool{
		{ID: "p1", Provider: types.ProviderClaude, Capacity: 5, Enabled: true, Role: types.RolePlan},
		{ID: "p2", Provider: types.ProviderClaude, Capacity: 5, Enabled: true, Role: types.RoleWork},
	})
	r.ReconcileActive(map[string]int{"p1": 3, "p2": 1})

	r.mu.RLock()
	a1 := r.pools["p1"].Active()
	a2 := r.pools["p2"].Active()
	r.mu.RUnlock()

	if a1 != 3 {
		t.Errorf("p1 active = %d, want 3", a1)
	}
	if a2 != 1 {
		t.Errorf("p2 active = %d, want 1", a2)
	}

	// A pool not present in the counts map is reset to 0.
	r.ReconcileActive(map[string]int{"p1": 0})
	r.mu.RLock()
	a1 = r.pools["p1"].Active()
	a2 = r.pools["p2"].Active()
	r.mu.RUnlock()
	if a1 != 0 || a2 != 0 {
		t.Errorf("after empty-ish reconcile: p1=%d p2=%d, want 0/0", a1, a2)
	}
}
