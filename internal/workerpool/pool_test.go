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
		{ID: "a", Provider: types.ProviderClaude, Capacity: 1, Enabled: true},
		{ID: "b", Provider: types.ProviderClaude, Capacity: 2, Enabled: true},
	})
	if got := r.FreeForSpawn(); got != 3 {
		t.Errorf("FreeForSpawn after Sync = %d, want 3", got)
	}

	// Drop one entry; remaining should keep its active count.
	if _, ok := r.AcquireFor(types.Workspace{}); !ok {
		t.Fatal("AcquireFor failed unexpectedly")
	}
	r.Sync([]types.Pool{
		{ID: "b", Provider: types.ProviderClaude, Capacity: 2, Enabled: true},
	})
	if got := r.FreeForSpawn(); got != 2 {
		t.Errorf("FreeForSpawn after dropping a = %d, want 2", got)
	}
}

func TestRegistryAcquireForRespectsCanSpawn(t *testing.T) {
	r := NewRegistry(func(p types.Provider) bool { return p == types.ProviderClaude })
	r.Sync([]types.Pool{
		{ID: "openai", Provider: types.ProviderOpenAI, Capacity: 5, Enabled: true},
		{ID: "claude", Provider: types.ProviderClaude, Capacity: 1, Enabled: true},
	})
	got, ok := r.AcquireFor(types.Workspace{})
	if !ok {
		t.Fatal("AcquireFor returned ok=false despite a Claude pool being eligible")
	}
	if got != "claude" {
		t.Fatalf("AcquireFor picked %q, want claude (only spawn-capable pool)", got)
	}
}

func TestRegistryAcquireSkipsDisabledAndZeroCapacity(t *testing.T) {
	r := NewRegistry(func(types.Provider) bool { return true })
	r.Sync([]types.Pool{
		{ID: "off", Provider: types.ProviderClaude, Capacity: 5, Enabled: false},
		{ID: "zero", Provider: types.ProviderClaude, Capacity: 0, Enabled: true},
		{ID: "ok", Provider: types.ProviderClaude, Capacity: 1, Enabled: true},
	})
	got, ok := r.AcquireFor(types.Workspace{})
	if !ok || got != "ok" {
		t.Fatalf("AcquireFor returned (%q, %v), want (ok, true)", got, ok)
	}
}

func TestRegistryActiveCount(t *testing.T) {
	r := NewRegistry(func(types.Provider) bool { return true })
	r.Sync([]types.Pool{
		{ID: "a", Provider: types.ProviderClaude, Capacity: 2, Enabled: true},
	})
	if r.ActiveCount("a") != 0 {
		t.Fatalf("initial ActiveCount = %d, want 0", r.ActiveCount("a"))
	}
	if _, ok := r.AcquireFor(types.Workspace{}); !ok {
		t.Fatal("AcquireFor failed")
	}
	if r.ActiveCount("a") != 1 {
		t.Fatalf("ActiveCount post-acquire = %d, want 1", r.ActiveCount("a"))
	}
	r.ReleaseByPool("a")
	if r.ActiveCount("a") != 0 {
		t.Fatalf("ActiveCount post-release = %d, want 0", r.ActiveCount("a"))
	}
}
