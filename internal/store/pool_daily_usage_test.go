package store

import (
	"testing"
	"time"
)

func TestUpsertPoolDailyUsage_Accumulates(t *testing.T) {
	s := openTempStore(t)
	p := seedPool(t, s)

	date := time.Now().UTC().Format("2006-01-02")

	if err := s.UpsertPoolDailyUsage(p.ID, date, 1, 50000, 12.5); err != nil {
		t.Fatalf("first upsert: %v", err)
	}
	if err := s.UpsertPoolDailyUsage(p.ID, date, 2, 30000, 8.0); err != nil {
		t.Fatalf("second upsert: %v", err)
	}

	var sessions int
	var tokens int64
	var cents float64
	err := s.DB.QueryRow(
		`SELECT sessions, tokens, cents FROM pool_daily_usage WHERE pool_id = ? AND date = ?`,
		p.ID, date,
	).Scan(&sessions, &tokens, &cents)
	if err != nil {
		t.Fatalf("query: %v", err)
	}
	if sessions != 3 {
		t.Errorf("sessions = %d, want 3 (accumulated)", sessions)
	}
	if tokens != 80000 {
		t.Errorf("tokens = %d, want 80000", tokens)
	}
	if cents != 20.5 {
		t.Errorf("cents = %f, want 20.5", cents)
	}
}

func TestUpsertPoolDailyUsage_Multipledays(t *testing.T) {
	s := openTempStore(t)
	p := seedPool(t, s)

	dates := []string{"2026-05-01", "2026-05-02", "2026-05-03"}
	for _, d := range dates {
		if err := s.UpsertPoolDailyUsage(p.ID, d, 1, 1000, 5.0); err != nil {
			t.Fatalf("upsert %s: %v", d, err)
		}
	}

	var count int
	err := s.DB.QueryRow(`SELECT COUNT(*) FROM pool_daily_usage WHERE pool_id = ?`, p.ID).Scan(&count)
	if err != nil {
		t.Fatalf("count: %v", err)
	}
	if count != 3 {
		t.Errorf("row count = %d, want 3", count)
	}
}

func TestPoolProjectionData_Empty(t *testing.T) {
	s := openTempStore(t)
	p := seedPool(t, s)

	c7, c30, sess30, days, err := s.PoolProjectionData(p.ID)
	if err != nil {
		t.Fatalf("PoolProjectionData: %v", err)
	}
	if c7 != 0 || c30 != 0 || sess30 != 0 || days != 0 {
		t.Errorf("expected all zeros for pool with no sessions, got c7=%v c30=%v sess=%d days=%d",
			c7, c30, sess30, days)
	}
}

func TestUpsertPoolDailyUsage_FreeTierProvider(t *testing.T) {
	// Upsert for a pool with zero cents (free tier). Should succeed and not
	// corrupt other pools.
	s := openTempStore(t)
	p := seedPool(t, s)

	date := "2026-05-10"
	if err := s.UpsertPoolDailyUsage(p.ID, date, 5, 100000, 0); err != nil {
		t.Fatalf("upsert: %v", err)
	}

	var cents float64
	err := s.DB.QueryRow(
		`SELECT cents FROM pool_daily_usage WHERE pool_id = ? AND date = ?`,
		p.ID, date,
	).Scan(&cents)
	if err != nil {
		t.Fatalf("query: %v", err)
	}
	if cents != 0 {
		t.Errorf("cents = %f, want 0 for free-tier pool", cents)
	}
}
