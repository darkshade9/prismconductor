package workerpool

import (
	"testing"

	"prismconductor/internal/types"
)

// TestAcquireForPlan_PrefersWorkspaceSpecific verifies that a workspace-specific
// pool bound to workspace A is selected before shared pools when workspace A
// requests a slot (issue #109 acceptance criterion 2).
func TestAcquireForPlan_PrefersWorkspaceSpecific(t *testing.T) {
	r := NewRegistry(func(types.Provider) bool { return true })
	r.Sync([]types.Pool{
		{
			ID: "shared", Provider: types.ProviderClaude, Capacity: 5,
			Enabled: true, Role: types.RolePlan,
			Scope: types.PoolScopeShared,
		},
		{
			ID: "ws-specific", Provider: types.ProviderClaude, Capacity: 1,
			Enabled: true, Role: types.RolePlan,
			Scope: types.PoolScopeWorkspace, WorkspaceID: "ws-a",
		},
	})
	ws := types.Workspace{ID: "ws-a"}
	id, ok := r.AcquireForPlan(ws)
	if !ok {
		t.Fatal("AcquireForPlan returned ok=false")
	}
	if id != "ws-specific" {
		t.Fatalf("AcquireForPlan picked %q, want ws-specific (workspace-specific pool)", id)
	}
}

// TestAcquireForPlan_FallsBackToShared verifies that when the workspace-specific
// pool is full, the acquire falls back to shared pools (issue #109 criterion 3).
func TestAcquireForPlan_FallsBackToShared(t *testing.T) {
	r := NewRegistry(func(types.Provider) bool { return true })
	r.Sync([]types.Pool{
		{
			ID: "shared", Provider: types.ProviderClaude, Capacity: 5,
			Enabled: true, Role: types.RolePlan,
			Scope: types.PoolScopeShared,
		},
		{
			ID: "ws-specific", Provider: types.ProviderClaude, Capacity: 1,
			Enabled: true, Role: types.RolePlan,
			Scope: types.PoolScopeWorkspace, WorkspaceID: "ws-a",
		},
	})
	ws := types.Workspace{ID: "ws-a"}

	// Fill the workspace-specific pool.
	first, ok := r.AcquireForPlan(ws)
	if !ok || first != "ws-specific" {
		t.Fatalf("first acquire = (%q, %v), want (ws-specific, true)", first, ok)
	}
	// Next acquire should fall through to shared.
	second, ok := r.AcquireForPlan(ws)
	if !ok {
		t.Fatal("second AcquireForPlan returned ok=false; expected fallback to shared pool")
	}
	if second != "shared" {
		t.Fatalf("second acquire = %q, want shared", second)
	}
}

// TestAcquireForPlan_OtherWorkspaceDoesNotSeeSpecific verifies that a pool bound
// to workspace A is NOT selected when workspace B makes a request; workspace B
// should only see shared pools (issue #109 criterion 2).
func TestAcquireForPlan_OtherWorkspaceDoesNotSeeSpecific(t *testing.T) {
	r := NewRegistry(func(types.Provider) bool { return true })
	r.Sync([]types.Pool{
		{
			ID: "shared", Provider: types.ProviderClaude, Capacity: 5,
			Enabled: true, Role: types.RolePlan,
			Scope: types.PoolScopeShared,
		},
		{
			ID: "ws-a-only", Provider: types.ProviderClaude, Capacity: 2,
			Enabled: true, Role: types.RolePlan,
			Scope: types.PoolScopeWorkspace, WorkspaceID: "ws-a",
		},
	})
	wsB := types.Workspace{ID: "ws-b"}
	id, ok := r.AcquireForPlan(wsB)
	if !ok {
		t.Fatal("AcquireForPlan returned ok=false for workspace B")
	}
	if id != "shared" {
		t.Fatalf("workspace B acquired %q, want shared (not ws-a-only)", id)
	}
}

// TestAcquireForWork_PrefersWorkspaceSpecific mirrors the plan test for work role.
func TestAcquireForWork_PrefersWorkspaceSpecific(t *testing.T) {
	r := NewRegistry(func(types.Provider) bool { return true })
	r.Sync([]types.Pool{
		{
			ID: "shared", Provider: types.ProviderClaude, Capacity: 5,
			Enabled: true, Role: types.RoleWork,
			Scope: types.PoolScopeShared,
		},
		{
			ID: "ws-specific", Provider: types.ProviderClaude, Capacity: 1,
			Enabled: true, Role: types.RoleWork,
			Scope: types.PoolScopeWorkspace, WorkspaceID: "ws-a",
		},
	})
	ws := types.Workspace{ID: "ws-a"}
	id, ok := r.AcquireForWork(ws)
	if !ok {
		t.Fatal("AcquireForWork returned ok=false")
	}
	if id != "ws-specific" {
		t.Fatalf("AcquireForWork picked %q, want ws-specific", id)
	}
}

// TestFreeWorkSlots_CountsAllScopes verifies that FreePlanSlots / FreeWorkSlots
// aggregate across both shared and workspace-specific pools (the orchestrator
// pre-check must see the full capacity picture).
func TestFreeSlots_CountsAllScopes(t *testing.T) {
	r := NewRegistry(func(types.Provider) bool { return true })
	r.Sync([]types.Pool{
		{
			ID: "shared", Provider: types.ProviderClaude, Capacity: 3,
			Enabled: true, Role: types.RolePlan,
			Scope: types.PoolScopeShared,
		},
		{
			ID: "ws-specific", Provider: types.ProviderClaude, Capacity: 2,
			Enabled: true, Role: types.RolePlan,
			Scope: types.PoolScopeWorkspace, WorkspaceID: "ws-a",
		},
	})
	if got := r.FreePlanSlots(); got != 5 {
		t.Fatalf("FreePlanSlots = %d, want 5 (3 shared + 2 workspace-specific)", got)
	}
}
