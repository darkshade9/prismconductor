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
