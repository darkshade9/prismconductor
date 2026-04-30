package types

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestSessionPoolIDOmitempty(t *testing.T) {
	s := Session{ID: "s1", WorkspaceID: "ws", IssueNumber: 1}
	b, err := json.Marshal(s)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if strings.Contains(string(b), "pool_id") {
		t.Errorf("expected omitempty to drop pool_id when empty; got %s", b)
	}
}

func TestSessionPoolIDRoundTrip(t *testing.T) {
	s := Session{ID: "s1", WorkspaceID: "ws", IssueNumber: 1, PoolID: "pool-abc"}
	b, err := json.Marshal(s)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var got Session
	if err := json.Unmarshal(b, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got.PoolID != "pool-abc" {
		t.Errorf("PoolID round-trip: got %q want %q", got.PoolID, "pool-abc")
	}
}

func TestSessionLegacyJSONNoPoolID(t *testing.T) {
	legacy := []byte(`{"id":"s1","workspace_id":"ws","issue_number":1,"mode":"plan","state":"completed","started_at":"2025-01-01T00:00:00Z","pid":0,"last_prompt":""}`)
	var s Session
	if err := json.Unmarshal(legacy, &s); err != nil {
		t.Fatalf("legacy unmarshal: %v", err)
	}
	if s.PoolID != "" {
		t.Errorf("expected empty PoolID for legacy session; got %q", s.PoolID)
	}
}
