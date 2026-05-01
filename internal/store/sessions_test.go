package store

import (
	"testing"
	"time"

	"prismconductor/internal/types"
)

func saveBlockedSession(t *testing.T, s *Store, wsID string, issueNum int, id string) {
	t.Helper()
	now := time.Now()
	ended := now.Add(-1 * time.Minute)
	sess := &types.Session{
		ID:            id,
		WorkspaceID:   wsID,
		IssueNumber:   issueNum,
		Mode:          types.ModePlan,
		State:         types.StateBlocked,
		StartedAt:     now.Add(-5 * time.Minute),
		EndedAt:       &ended,
		BlockedReason: "BLOCKED: something went wrong",
	}
	if err := s.SaveSession(sess, ""); err != nil {
		t.Fatalf("SaveSession: %v", err)
	}
	if err := s.UpdateSessionState(id, types.StateBlocked); err != nil {
		t.Fatalf("UpdateSessionState: %v", err)
	}
}

// UpdateSessionState must keep `state` column AND `$.state` inside the JSON
// blob in lock-step. ListSessionsForIssue (and the IssueView assembler that
// drives card glow) reads state from the JSON blob — if the blob lags
// behind the indexed column, terminal sessions surface as "running"
// forever, producing the zombie-glow class of bug.
func TestUpdateSessionState_PatchesJSONBlob(t *testing.T) {
	s := newTestStore(t)
	now := time.Now()
	sess := &types.Session{
		ID:          "sess-1",
		WorkspaceID: "ws1",
		IssueNumber: 1,
		Mode:        types.ModeExecute,
		State:       types.StateRunning,
		StartedAt:   now,
	}
	if err := s.SaveSession(sess, ""); err != nil {
		t.Fatalf("SaveSession: %v", err)
	}

	if err := s.UpdateSessionState("sess-1", types.StateCompleted); err != nil {
		t.Fatalf("UpdateSessionState: %v", err)
	}

	got, err := s.ListSessionsForIssue("ws1", 1)
	if err != nil {
		t.Fatalf("ListSessionsForIssue: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("got %d sessions, want 1", len(got))
	}
	if got[0].State != types.StateCompleted {
		t.Fatalf("state from JSON blob = %q, want %q (json_set drift bug)",
			got[0].State, types.StateCompleted)
	}
}

func TestAcknowledgeLatestFailure_SetsAcknowledgedAt(t *testing.T) {
	s := newTestStore(t)
	saveBlockedSession(t, s, "ws1", 10, "sess-1")

	got, err := s.AcknowledgeLatestFailure("ws1", 10)
	if err != nil {
		t.Fatalf("AcknowledgeLatestFailure: %v", err)
	}
	if got == nil {
		t.Fatal("expected a session back, got nil")
	}
	if got.AcknowledgedAt == nil {
		t.Fatal("AcknowledgedAt should be set")
	}
	if *got.AcknowledgedAt <= 0 {
		t.Fatalf("AcknowledgedAt should be a positive Unix epoch, got %d", *got.AcknowledgedAt)
	}
}

func TestAcknowledgeLatestFailure_NoRowReturnsNil(t *testing.T) {
	s := newTestStore(t)
	got, err := s.AcknowledgeLatestFailure("ws1", 99)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != nil {
		t.Fatalf("expected nil session for unknown issue, got %+v", got)
	}
}

func TestAcknowledgeLatestFailure_IdempotentSecondCall(t *testing.T) {
	s := newTestStore(t)
	saveBlockedSession(t, s, "ws1", 10, "sess-1")

	if _, err := s.AcknowledgeLatestFailure("ws1", 10); err != nil {
		t.Fatalf("first call: %v", err)
	}
	// Second call should find no unacknowledged row and return nil.
	got, err := s.AcknowledgeLatestFailure("ws1", 10)
	if err != nil {
		t.Fatalf("second call: %v", err)
	}
	if got != nil {
		t.Fatalf("second call should return nil (already acknowledged), got %+v", got)
	}
}

func TestAcknowledgeLatestFailure_MostRecentOnly(t *testing.T) {
	s := newTestStore(t)
	saveBlockedSession(t, s, "ws1", 10, "sess-old")

	// Add a newer blocked session.
	now := time.Now()
	ended := now.Add(-30 * time.Second)
	newer := &types.Session{
		ID:            "sess-new",
		WorkspaceID:   "ws1",
		IssueNumber:   10,
		Mode:          types.ModeExecute,
		State:         types.StateBlocked,
		StartedAt:     now.Add(-2 * time.Minute),
		EndedAt:       &ended,
		BlockedReason: "BLOCKED: newer failure",
	}
	if err := s.SaveSession(newer, ""); err != nil {
		t.Fatalf("SaveSession newer: %v", err)
	}
	if err := s.UpdateSessionState("sess-new", types.StateBlocked); err != nil {
		t.Fatalf("UpdateSessionState: %v", err)
	}

	got, err := s.AcknowledgeLatestFailure("ws1", 10)
	if err != nil {
		t.Fatalf("AcknowledgeLatestFailure: %v", err)
	}
	if got == nil {
		t.Fatal("expected a session")
	}
	if got.ID != "sess-new" {
		t.Fatalf("expected newest session sess-new, got %s", got.ID)
	}
}

func TestLoadRunningSessions_SkipsAcknowledged(t *testing.T) {
	s := newTestStore(t)
	saveBlockedSession(t, s, "ws1", 10, "sess-ack")

	if _, err := s.AcknowledgeLatestFailure("ws1", 10); err != nil {
		t.Fatalf("AcknowledgeLatestFailure: %v", err)
	}

	sessions, _, err := s.LoadRunningSessions()
	if err != nil {
		t.Fatalf("LoadRunningSessions: %v", err)
	}
	for _, sess := range sessions {
		if sess.ID == "sess-ack" {
			t.Fatal("acknowledged session should not appear in LoadRunningSessions")
		}
	}
}
