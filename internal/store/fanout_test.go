package store

import (
	"testing"
	"time"

	"prismconductor/internal/types"
)

func openFanoutTestStore(t *testing.T) *Store {
	t.Helper()
	dir := t.TempDir()
	st, err := Open(dir)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { st.DB.Close() })
	return st
}

func newProposal(id, srcWS, tgtWS string, srcIssue, srcPR int) types.FanoutProposal {
	return types.FanoutProposal{
		ID:                id,
		SourceWorkspaceID: srcWS,
		SourceIssueNumber: srcIssue,
		SourcePRNumber:    srcPR,
		TargetWorkspaceID: tgtWS,
		Title:             "Update callers of Foo",
		Body:              "Foo signature changed.",
		Labels:            []string{"cross-repo"},
		Status:            types.FanoutStatusPending,
		CreatedAt:         time.Now().Truncate(time.Second),
	}
}

func TestFanoutProposal_SaveAndList(t *testing.T) {
	st := openFanoutTestStore(t)

	p := newProposal("p1", "ws-a", "ws-b", 42, 7)
	if err := st.SaveFanoutProposal(p); err != nil {
		t.Fatalf("SaveFanoutProposal: %v", err)
	}

	got, err := st.ListFanoutProposals("ws-a", 42)
	if err != nil {
		t.Fatalf("ListFanoutProposals: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("want 1 proposal, got %d", len(got))
	}
	if got[0].ID != "p1" || got[0].Title != p.Title {
		t.Errorf("proposal mismatch: %+v", got[0])
	}
	if got[0].Status != types.FanoutStatusPending {
		t.Errorf("want pending, got %q", got[0].Status)
	}
}

func TestFanoutProposal_ApproveSetsStatus(t *testing.T) {
	st := openFanoutTestStore(t)

	p := newProposal("p2", "ws-a", "ws-b", 1, 3)
	if err := st.SaveFanoutProposal(p); err != nil {
		t.Fatalf("save: %v", err)
	}

	if err := st.ApproveFanoutProposal("p2", 99, "https://github.com/org/repo/issues/99"); err != nil {
		t.Fatalf("approve: %v", err)
	}

	got, _ := st.GetFanoutProposal("p2")
	if got.Status != types.FanoutStatusApproved {
		t.Errorf("want approved, got %q", got.Status)
	}
	if got.FiledIssueNumber == nil || *got.FiledIssueNumber != 99 {
		t.Errorf("want filed_issue_number=99, got %v", got.FiledIssueNumber)
	}
	if got.FiledIssueURL != "https://github.com/org/repo/issues/99" {
		t.Errorf("unexpected url: %q", got.FiledIssueURL)
	}
}

func TestFanoutProposal_DismissSetsStatus(t *testing.T) {
	st := openFanoutTestStore(t)

	p := newProposal("p3", "ws-a", "ws-b", 2, 5)
	if err := st.SaveFanoutProposal(p); err != nil {
		t.Fatalf("save: %v", err)
	}
	if err := st.DismissFanoutProposal("p3"); err != nil {
		t.Fatalf("dismiss: %v", err)
	}
	got, _ := st.GetFanoutProposal("p3")
	if got.Status != types.FanoutStatusDismissed {
		t.Errorf("want dismissed, got %q", got.Status)
	}
}

func TestFanoutProposal_IdempotentSave(t *testing.T) {
	st := openFanoutTestStore(t)

	p := newProposal("p4", "ws-a", "ws-b", 10, 11)
	_ = st.SaveFanoutProposal(p)
	// Overwrite with updated title.
	p.Title = "Updated title"
	if err := st.SaveFanoutProposal(p); err != nil {
		t.Fatalf("second save: %v", err)
	}
	got, _ := st.ListFanoutProposals("ws-a", 10)
	if len(got) != 1 {
		t.Fatalf("want 1, got %d", len(got))
	}
	if got[0].Title != "Updated title" {
		t.Errorf("title not updated: %q", got[0].Title)
	}
}

func TestClearExternalDep(t *testing.T) {
	st := openFanoutTestStore(t)

	// Save an issue with DependsOnExternal.
	iss := types.Issue{
		Number:      42,
		WorkspaceID: "ws-b",
		Title:       "Follow-on work",
		State:       "open",
		Column:      types.ColTodo,
		UpdatedAt:   time.Now(),
		DependsOnExternal: &types.ExternalDep{
			SourceWorkspaceID: "ws-a",
			SourceIssueNumber: 10,
			SourcePRNumber:    7,
		},
	}
	if _, err := st.SaveIssue(iss); err != nil {
		t.Fatalf("SaveIssue: %v", err)
	}

	// Clear the dep.
	if err := st.ClearExternalDep("ws-a", 7); err != nil {
		t.Fatalf("ClearExternalDep: %v", err)
	}

	// Reload and verify resolved.
	loaded, err := st.LoadIssue("ws-b", 42)
	if err != nil {
		t.Fatalf("LoadIssue: %v", err)
	}
	if loaded.DependsOnExternal == nil {
		t.Fatal("DependsOnExternal was nil after ClearExternalDep")
	}
	if !loaded.DependsOnExternal.Resolved {
		t.Error("DependsOnExternal.Resolved was false after ClearExternalDep")
	}
}
