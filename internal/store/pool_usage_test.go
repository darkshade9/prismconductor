package store

import (
	"testing"
	"time"

	"prismconductor/internal/types"
)

func seedPool(t *testing.T, s *Store) types.Pool {
	t.Helper()
	p := types.Pool{
		ID:        "pool-claude",
		Name:      "Claude Default",
		Provider:  types.ProviderClaude,
		Model:     "claude-sonnet-4-6",
		Capacity:  1,
		Enabled:   true,
		Role:      types.RoleWork,
		CreatedAt: time.Unix(1700000000, 0),
	}
	if err := s.SavePool(p); err != nil {
		t.Fatalf("SavePool: %v", err)
	}
	return p
}

func TestUpsertAndListPoolUsage(t *testing.T) {
	s := openTempStore(t)
	p := seedPool(t, s)

	now := time.Unix(1700001000, 0)
	resets := now.Add(5 * time.Minute)

	u := types.PoolUsage{
		PoolID:     p.ID,
		Window:     "requests",
		LimitValue: 500,
		Used:       10,
		ResetsAt:   resets,
		CapturedAt: now,
	}
	if err := s.UpsertPoolUsage(u); err != nil {
		t.Fatalf("UpsertPoolUsage: %v", err)
	}

	rows, err := s.ListPoolUsage()
	if err != nil {
		t.Fatalf("ListPoolUsage: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("expected 1 row, got %d", len(rows))
	}

	got := rows[0]
	if got.PoolID != p.ID {
		t.Errorf("pool_id = %q, want %q", got.PoolID, p.ID)
	}
	if got.PoolName != p.Name {
		t.Errorf("pool_name = %q, want %q", got.PoolName, p.Name)
	}
	if got.Window != "requests" {
		t.Errorf("window = %q, want requests", got.Window)
	}
	if got.LimitValue != 500 || got.Used != 10 {
		t.Errorf("limit=%d used=%d, want 500/10", got.LimitValue, got.Used)
	}
	// Timestamp precision: stored as Unix seconds.
	if got.ResetsAt.Unix() != resets.Unix() {
		t.Errorf("resets_at = %v, want %v", got.ResetsAt, resets)
	}
}

func TestUpsertPoolUsage_Overwrites(t *testing.T) {
	s := openTempStore(t)
	p := seedPool(t, s)

	now := time.Unix(1700001000, 0)

	first := types.PoolUsage{
		PoolID: p.ID, Window: "tokens",
		LimitValue: 100000, Used: 5000, ResetsAt: now.Add(time.Hour), CapturedAt: now,
	}
	if err := s.UpsertPoolUsage(first); err != nil {
		t.Fatalf("first upsert: %v", err)
	}

	second := types.PoolUsage{
		PoolID: p.ID, Window: "tokens",
		LimitValue: 100000, Used: 80000, ResetsAt: now.Add(2 * time.Hour), CapturedAt: now.Add(time.Minute),
	}
	if err := s.UpsertPoolUsage(second); err != nil {
		t.Fatalf("second upsert: %v", err)
	}

	rows, err := s.ListPoolUsage()
	if err != nil {
		t.Fatalf("ListPoolUsage: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("expected 1 row after overwrite, got %d", len(rows))
	}
	if rows[0].Used != 80000 {
		t.Errorf("used = %d, want 80000 (overwrite should win)", rows[0].Used)
	}
}

func TestListPoolUsage_SkipsOrphan(t *testing.T) {
	s := openTempStore(t)

	// Insert a usage row for a non-existent pool.
	u := types.PoolUsage{
		PoolID: "ghost-pool", Window: "requests",
		LimitValue: 100, Used: 10,
		ResetsAt: time.Now().Add(time.Hour), CapturedAt: time.Now(),
	}
	if err := s.UpsertPoolUsage(u); err != nil {
		t.Fatalf("UpsertPoolUsage: %v", err)
	}

	rows, err := s.ListPoolUsage()
	if err != nil {
		t.Fatalf("ListPoolUsage: %v", err)
	}
	// JOIN with pools filters the orphaned row.
	if len(rows) != 0 {
		t.Errorf("expected 0 rows (orphan filtered), got %d", len(rows))
	}
}
