package store

import (
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
