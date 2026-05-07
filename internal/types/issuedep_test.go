package types

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestIssueDepListUnmarshalLegacyInts(t *testing.T) {
	raw := []byte(`[1, 2, 3]`)
	var got IssueDepList
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatalf("unmarshal legacy ints: %v", err)
	}
	if len(got) != 3 {
		t.Fatalf("len = %d, want 3", len(got))
	}
	for i, want := range []int{1, 2, 3} {
		if got[i].WorkspaceID != "" {
			t.Errorf("[%d].WorkspaceID = %q, want empty", i, got[i].WorkspaceID)
		}
		if got[i].Number != want {
			t.Errorf("[%d].Number = %d, want %d", i, got[i].Number, want)
		}
	}
}

func TestIssueDepListUnmarshalNewShape(t *testing.T) {
	raw := []byte(`[{"workspace_id":"infra","number":5},{"workspace_id":"","number":2}]`)
	var got IssueDepList
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatalf("unmarshal new shape: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("len = %d, want 2", len(got))
	}
	if got[0].WorkspaceID != "infra" || got[0].Number != 5 {
		t.Errorf("[0] = %+v, want {infra 5}", got[0])
	}
	if got[1].WorkspaceID != "" || got[1].Number != 2 {
		t.Errorf("[1] = %+v, want { 2}", got[1])
	}
}

func TestIssueDepListMarshalShape(t *testing.T) {
	deps := IssueDepList{
		{WorkspaceID: "infra", Number: 7},
		{WorkspaceID: "", Number: 3},
	}
	b, err := json.Marshal(deps)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	// Unmarshal back and verify round-trip.
	var got IssueDepList
	if err := json.Unmarshal(b, &got); err != nil {
		t.Fatalf("round-trip unmarshal: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("round-trip len = %d, want 2", len(got))
	}
	if got[0] != deps[0] || got[1] != deps[1] {
		t.Errorf("round-trip mismatch: got %+v, want %+v", got, deps)
	}
}

func TestIssueDepListUnmarshalEmpty(t *testing.T) {
	raw := []byte(`[]`)
	var got IssueDepList
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatalf("unmarshal empty: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("len = %d, want 0", len(got))
	}
}

func TestIssueDepListUnmarshalNull(t *testing.T) {
	raw := []byte(`null`)
	var got IssueDepList
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatalf("unmarshal null: %v", err)
	}
	if got != nil {
		t.Fatalf("expected nil IssueDepList for JSON null, got %+v", got)
	}
}

func TestIssueWaitingForDepField(t *testing.T) {
	iss := Issue{
		WorkspaceID: "ws1",
		Number:      1,
		Title:       "test",
		State:       "open",
		WaitingForDep: &IssueDep{WorkspaceID: "infra", Number: 42},
	}
	b, err := json.Marshal(iss)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var got Issue
	if err := json.Unmarshal(b, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got.WaitingForDep == nil {
		t.Fatal("WaitingForDep lost after round-trip")
	}
	if got.WaitingForDep.WorkspaceID != "infra" || got.WaitingForDep.Number != 42 {
		t.Errorf("WaitingForDep = %+v, want {infra 42}", got.WaitingForDep)
	}
}

func TestIssueWaitingForDepOmitsWhenNil(t *testing.T) {
	iss := Issue{WorkspaceID: "ws1", Number: 1, Title: "test", State: "open"}
	b, err := json.Marshal(iss)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	s := string(b)
	if strings.Contains(s, "waiting_for_dep") {
		t.Errorf("expected waiting_for_dep to be omitted when nil; got %s", s)
	}
}
