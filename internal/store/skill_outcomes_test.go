package store

import (
	"testing"
	"time"

	"prismconductor/internal/types"
)

func TestSkillOutcomes_RoundTrip(t *testing.T) {
	s := openTempStore(t)

	now := time.Now().Unix()
	o := types.SkillOutcome{
		SessionID:      "sess-1",
		WorkspaceID:    "ws1",
		IssueNumber:    42,
		SkillPath:      "bundled:conductor-plan",
		SkillHash:      "abc123",
		Mode:           "plan",
		Outcome:        "success",
		BlockedReason:  "",
		UserAction:     "",
		CostCents:      1.5,
		DurationMs:     30000,
		TranscriptPath: "/tmp/t.log",
		CapturedAt:     now,
	}

	if err := s.RecordSkillOutcome(o); err != nil {
		t.Fatalf("RecordSkillOutcome: %v", err)
	}

	rows, err := s.ListSkillOutcomes("bundled:conductor-plan", now-1, 10)
	if err != nil {
		t.Fatalf("ListSkillOutcomes: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("ListSkillOutcomes: got %d rows, want 1", len(rows))
	}
	got := rows[0]
	if got.SessionID != "sess-1" || got.Outcome != "success" || got.SkillHash != "abc123" {
		t.Errorf("unexpected row: %+v", got)
	}
}

func TestSkillOutcomes_IdempotentUpsert(t *testing.T) {
	s := openTempStore(t)

	now := time.Now().Unix()
	o := types.SkillOutcome{
		SessionID:   "sess-dup",
		WorkspaceID: "ws1",
		IssueNumber: 10,
		SkillPath:   "bundled:conductor-execute",
		SkillHash:   "deadbeef",
		Mode:        "execute",
		Outcome:     "failed",
		CapturedAt:  now,
	}

	if err := s.RecordSkillOutcome(o); err != nil {
		t.Fatalf("first insert: %v", err)
	}
	// Second call with a different outcome should upsert (update outcome).
	o.Outcome = "success"
	if err := s.RecordSkillOutcome(o); err != nil {
		t.Fatalf("second upsert: %v", err)
	}

	rows, err := s.ListSkillOutcomes("bundled:conductor-execute", now-1, 10)
	if err != nil {
		t.Fatalf("ListSkillOutcomes: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("expected 1 row after upsert, got %d", len(rows))
	}
	if rows[0].Outcome != "success" {
		t.Errorf("outcome = %q, want %q", rows[0].Outcome, "success")
	}
}

func TestSkillOutcomes_SummarizeCounts(t *testing.T) {
	s := openTempStore(t)

	now := time.Now().Unix()
	outcomes := []struct {
		id      string
		outcome string
	}{
		{"s1", "success"},
		{"s2", "success"},
		{"s3", "blocked"},
		{"s4", "failed"},
	}
	for _, row := range outcomes {
		if err := s.RecordSkillOutcome(types.SkillOutcome{
			SessionID:   row.id,
			WorkspaceID: "ws1",
			IssueNumber: 99,
			SkillPath:   "bundled:conductor-plan",
			SkillHash:   "hash",
			Mode:        "plan",
			Outcome:     row.outcome,
			CapturedAt:  now,
		}); err != nil {
			t.Fatalf("RecordSkillOutcome %s: %v", row.id, err)
		}
	}

	sum, err := s.SummarizeOutcomes("bundled:conductor-plan", now-1)
	if err != nil {
		t.Fatalf("SummarizeOutcomes: %v", err)
	}
	if sum.TotalSessions != 4 {
		t.Errorf("total = %d, want 4", sum.TotalSessions)
	}
	if sum.Counts["success"] != 2 {
		t.Errorf("success count = %d, want 2", sum.Counts["success"])
	}
	if sum.Counts["blocked"] != 1 {
		t.Errorf("blocked count = %d, want 1", sum.Counts["blocked"])
	}
	if sum.Counts["failed"] != 1 {
		t.Errorf("failed count = %d, want 1", sum.Counts["failed"])
	}
}

func TestSkillOutcomes_WindowFilter(t *testing.T) {
	s := openTempStore(t)

	old := time.Now().Unix() - 100
	recent := time.Now().Unix()

	for _, row := range []struct {
		id  string
		at  int64
	}{
		{"old-sess", old},
		{"new-sess", recent},
	} {
		if err := s.RecordSkillOutcome(types.SkillOutcome{
			SessionID:   row.id,
			WorkspaceID: "ws1",
			IssueNumber: 1,
			SkillPath:   "bundled:conductor-plan",
			SkillHash:   "x",
			Mode:        "plan",
			Outcome:     "success",
			CapturedAt:  row.at,
		}); err != nil {
			t.Fatalf("insert %s: %v", row.id, err)
		}
	}

	// Only rows at or after 'recent' should come back.
	rows, err := s.ListSkillOutcomes("bundled:conductor-plan", recent, 10)
	if err != nil {
		t.Fatalf("ListSkillOutcomes: %v", err)
	}
	if len(rows) != 1 || rows[0].SessionID != "new-sess" {
		t.Errorf("expected only new-sess, got %v", rows)
	}
}
