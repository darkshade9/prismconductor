package store

import (
	"errors"
	"fmt"
	"path/filepath"
	"testing"
	"time"

	"prismconductor/internal/types"
)

func openTempStore(t *testing.T) *Store {
	t.Helper()
	dir := t.TempDir()
	s, err := Open(filepath.Join(dir))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return s
}

func TestPoolsCRUDRoundtrip(t *testing.T) {
	s := openTempStore(t)
	if n, err := s.PoolsCount(); err != nil || n != 0 {
		t.Fatalf("PoolsCount on fresh DB = (%d, %v), want (0, nil)", n, err)
	}
	now := time.Unix(1700000000, 0)
	p := types.Pool{
		ID:        "pool-1",
		Name:      "claude-default",
		Provider:  types.ProviderClaude,
		Endpoint:  "",
		Model:     "claude-opus-4-7",
		Capacity:  3,
		Enabled:   true,
		APIKey:    "",
		CreatedAt: now,
	}
	if err := s.SavePool(p); err != nil {
		t.Fatalf("SavePool: %v", err)
	}
	got, err := s.GetPool(p.ID)
	if err != nil {
		t.Fatalf("GetPool: %v", err)
	}
	if got.Name != p.Name || got.Provider != p.Provider || got.Model != p.Model || got.Capacity != p.Capacity || !got.Enabled {
		t.Fatalf("GetPool round-trip mismatch: %+v", got)
	}
	if !got.CreatedAt.Equal(now) {
		t.Errorf("CreatedAt = %v, want %v", got.CreatedAt, now)
	}

	// Upsert: change capacity.
	got.Capacity = 5
	if err := s.SavePool(got); err != nil {
		t.Fatalf("SavePool upsert: %v", err)
	}
	again, _ := s.GetPool(p.ID)
	if again.Capacity != 5 {
		t.Errorf("Capacity after upsert = %d, want 5", again.Capacity)
	}

	if n, err := s.PoolsCount(); err != nil || n != 1 {
		t.Fatalf("PoolsCount = (%d, %v), want (1, nil)", n, err)
	}

	if err := s.DeletePool(p.ID); err != nil {
		t.Fatalf("DeletePool: %v", err)
	}
	if n, _ := s.PoolsCount(); n != 0 {
		t.Errorf("PoolsCount after delete = %d, want 0", n)
	}
}

func TestListPoolsOrdering(t *testing.T) {
	s := openTempStore(t)
	t1 := time.Unix(1700000000, 0)
	t2 := time.Unix(1700000100, 0)
	_ = s.SavePool(types.Pool{ID: "later", Provider: types.ProviderClaude, Capacity: 1, CreatedAt: t2, Model: "m"})
	_ = s.SavePool(types.Pool{ID: "earlier", Provider: types.ProviderClaude, Capacity: 1, CreatedAt: t1, Model: "m"})
	rows, err := s.ListPools()
	if err != nil {
		t.Fatalf("ListPools: %v", err)
	}
	if len(rows) != 2 {
		t.Fatalf("ListPools returned %d rows, want 2", len(rows))
	}
	if rows[0].ID != "earlier" {
		t.Errorf("rows[0] = %s, want earlier (ordered by created_at asc)", rows[0].ID)
	}
}

func TestCreatePool_AssignsNextPriority(t *testing.T) {
	s := openTempStore(t)
	now := time.Now()
	ids := []string{"p1", "p2", "p3"}
	for i, id := range ids {
		p := types.Pool{
			ID:        id,
			Provider:  types.ProviderClaude,
			Model:     "m",
			Capacity:  1,
			Enabled:   true,
			Role:      types.RoleWork,
			CreatedAt: now.Add(time.Duration(i) * time.Second),
		}
		if err := s.CreatePool(p); err != nil {
			t.Fatalf("CreatePool %s: %v", id, err)
		}
	}
	for i, id := range ids {
		got, err := s.GetPool(id)
		if err != nil {
			t.Fatalf("GetPool %s: %v", id, err)
		}
		if got.Priority != i {
			t.Errorf("pool %s: priority = %d, want %d", id, got.Priority, i)
		}
	}
}

func TestCreatePool_IgnoresUserSuppliedPriority(t *testing.T) {
	s := openTempStore(t)
	p := types.Pool{ID: "p1", Provider: types.ProviderClaude, Model: "m", Capacity: 1, Enabled: true, Role: types.RoleWork, Priority: 99, CreatedAt: time.Now()}
	if err := s.CreatePool(p); err != nil {
		t.Fatalf("CreatePool: %v", err)
	}
	got, _ := s.GetPool("p1")
	if got.Priority != 0 {
		t.Errorf("Priority = %d, want 0 (MAX+1 with no enabled peers)", got.Priority)
	}
}

func TestCreatePool_OnlyConsidersEnabledSameRole(t *testing.T) {
	s := openTempStore(t)
	now := time.Now()
	// Disabled work pool at priority=5 — must not count toward MAX.
	disabled := types.Pool{ID: "disabled-work", Provider: types.ProviderClaude, Model: "m", Capacity: 1, Enabled: false, Role: types.RoleWork, Priority: 5, CreatedAt: now}
	if err := s.SavePool(disabled); err != nil {
		t.Fatalf("SavePool disabled: %v", err)
	}
	// Enabled plan pool at priority=10 — different role, must be ignored.
	plan := types.Pool{ID: "plan-pool", Provider: types.ProviderClaude, Model: "m", Capacity: 1, Enabled: true, Role: types.RolePlan, Priority: 10, CreatedAt: now}
	if err := s.SavePool(plan); err != nil {
		t.Fatalf("SavePool plan: %v", err)
	}
	// New enabled work pool — no enabled work peers → priority=0.
	newWork := types.Pool{ID: "new-work", Provider: types.ProviderClaude, Model: "m", Capacity: 1, Enabled: true, Role: types.RoleWork, CreatedAt: now.Add(time.Second)}
	if err := s.CreatePool(newWork); err != nil {
		t.Fatalf("CreatePool: %v", err)
	}
	got, _ := s.GetPool("new-work")
	if got.Priority != 0 {
		t.Errorf("Priority = %d, want 0 (disabled peer excluded from MAX)", got.Priority)
	}
}

func TestCreatePool_RejectsDuplicateID(t *testing.T) {
	s := openTempStore(t)
	p := types.Pool{ID: "dup", Provider: types.ProviderClaude, Model: "m", Capacity: 1, Enabled: true, Role: types.RoleWork, CreatedAt: time.Now()}
	if err := s.CreatePool(p); err != nil {
		t.Fatalf("first CreatePool: %v", err)
	}
	if err := s.CreatePool(p); !errors.Is(err, ErrPoolExists) {
		t.Errorf("second CreatePool: got %v, want ErrPoolExists", err)
	}
}

func TestSavePool_PreservesPriorityOnUpdate(t *testing.T) {
	s := openTempStore(t)
	p := types.Pool{ID: "p1", Provider: types.ProviderClaude, Model: "m", Capacity: 1, Enabled: true, Role: types.RoleWork, CreatedAt: time.Now()}
	if err := s.CreatePool(p); err != nil {
		t.Fatalf("CreatePool: %v", err)
	}
	got, _ := s.GetPool("p1")
	got.Priority = 7
	if err := s.SavePool(got); err != nil {
		t.Fatalf("SavePool: %v", err)
	}
	after, _ := s.GetPool("p1")
	if after.Priority != 7 {
		t.Errorf("Priority after SavePool = %d, want 7", after.Priority)
	}
}

func TestReconcilePoolPriorities_TiedAtZero(t *testing.T) {
	s := openTempStore(t)
	t1 := time.Unix(1700000000, 0)
	t2 := time.Unix(1700000100, 0)
	_ = s.SavePool(types.Pool{ID: "older", Provider: types.ProviderClaude, Model: "m", Capacity: 1, Enabled: true, Role: types.RoleWork, Priority: 0, CreatedAt: t1})
	_ = s.SavePool(types.Pool{ID: "newer", Provider: types.ProviderClaude, Model: "m", Capacity: 1, Enabled: true, Role: types.RoleWork, Priority: 0, CreatedAt: t2})

	n, err := s.ReconcilePoolPriorities()
	if err != nil {
		t.Fatalf("ReconcilePoolPriorities: %v", err)
	}
	if n != 2 {
		t.Errorf("mutated rows = %d, want 2", n)
	}
	olderAfter, _ := s.GetPool("older")
	newerAfter, _ := s.GetPool("newer")
	if olderAfter.Priority != 0 {
		t.Errorf("older.Priority = %d, want 0", olderAfter.Priority)
	}
	if newerAfter.Priority != 1 {
		t.Errorf("newer.Priority = %d, want 1", newerAfter.Priority)
	}
}

func TestReconcilePoolPriorities_AlreadyOrdered_Noop(t *testing.T) {
	s := openTempStore(t)
	for i := 0; i < 3; i++ {
		p := types.Pool{
			ID:       fmt.Sprintf("p%d", i),
			Provider: types.ProviderClaude,
			Model:    "m",
			Capacity: 1,
			Enabled:  true,
			Role:     types.RoleWork,
			Priority: i,
			CreatedAt: time.Unix(1700000000+int64(i)*100, 0),
		}
		if err := s.SavePool(p); err != nil {
			t.Fatalf("SavePool p%d: %v", i, err)
		}
	}
	n, err := s.ReconcilePoolPriorities()
	if err != nil {
		t.Fatalf("ReconcilePoolPriorities: %v", err)
	}
	if n != 0 {
		t.Errorf("mutated rows = %d, want 0 (no ties)", n)
	}
	for i := 0; i < 3; i++ {
		got, _ := s.GetPool(fmt.Sprintf("p%d", i))
		if got.Priority != i {
			t.Errorf("p%d.Priority = %d, want %d", i, got.Priority, i)
		}
	}
}

func TestReconcilePoolPriorities_PerRoleScope(t *testing.T) {
	s := openTempStore(t)
	t1 := time.Unix(1700000000, 0)
	t2 := time.Unix(1700000100, 0)
	// Two work pools tied at 0.
	_ = s.SavePool(types.Pool{ID: "w1", Provider: types.ProviderClaude, Model: "m", Capacity: 1, Enabled: true, Role: types.RoleWork, Priority: 0, CreatedAt: t1})
	_ = s.SavePool(types.Pool{ID: "w2", Provider: types.ProviderClaude, Model: "m", Capacity: 1, Enabled: true, Role: types.RoleWork, Priority: 0, CreatedAt: t2})
	// Plan pools already at 0,1 — no ties.
	_ = s.SavePool(types.Pool{ID: "plan1", Provider: types.ProviderClaude, Model: "m", Capacity: 1, Enabled: true, Role: types.RolePlan, Priority: 0, CreatedAt: t1})
	_ = s.SavePool(types.Pool{ID: "plan2", Provider: types.ProviderClaude, Model: "m", Capacity: 1, Enabled: true, Role: types.RolePlan, Priority: 1, CreatedAt: t2})

	n, err := s.ReconcilePoolPriorities()
	if err != nil {
		t.Fatalf("ReconcilePoolPriorities: %v", err)
	}
	if n != 2 {
		t.Errorf("mutated rows = %d, want 2 (work role only)", n)
	}
	p1, _ := s.GetPool("plan1")
	p2, _ := s.GetPool("plan2")
	if p1.Priority != 0 || p2.Priority != 1 {
		t.Errorf("plan priorities changed: plan1=%d plan2=%d, want 0,1", p1.Priority, p2.Priority)
	}
}

func TestReconcilePoolPriorities_Idempotent(t *testing.T) {
	s := openTempStore(t)
	t1 := time.Unix(1700000000, 0)
	t2 := time.Unix(1700000100, 0)
	_ = s.SavePool(types.Pool{ID: "w1", Provider: types.ProviderClaude, Model: "m", Capacity: 1, Enabled: true, Role: types.RoleWork, Priority: 0, CreatedAt: t1})
	_ = s.SavePool(types.Pool{ID: "w2", Provider: types.ProviderClaude, Model: "m", Capacity: 1, Enabled: true, Role: types.RoleWork, Priority: 0, CreatedAt: t2})

	n1, err := s.ReconcilePoolPriorities()
	if err != nil {
		t.Fatalf("first reconcile: %v", err)
	}
	if n1 != 2 {
		t.Errorf("first reconcile: mutated %d rows, want 2", n1)
	}
	n2, err := s.ReconcilePoolPriorities()
	if err != nil {
		t.Fatalf("second reconcile: %v", err)
	}
	if n2 != 0 {
		t.Errorf("second reconcile: mutated %d rows, want 0", n2)
	}
}
