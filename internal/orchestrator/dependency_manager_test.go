package orchestrator

import (
	"testing"
	"time"

	"prismconductor/internal/types"
)

// fakeDepLoader implements CrossDepLoader for tests.
type fakeDepLoader struct {
	issues map[string]map[int]types.Issue // workspace_id -> number -> issue
}

func (f *fakeDepLoader) LoadIssue(workspaceID string, number int) (types.Issue, error) {
	if ws, ok := f.issues[workspaceID]; ok {
		if iss, ok := ws[number]; ok {
			return iss, nil
		}
	}
	return types.Issue{}, &issueNotFoundError{workspaceID: workspaceID, number: number}
}

type issueNotFoundError struct {
	workspaceID string
	number      int
}

func (e *issueNotFoundError) Error() string {
	return "issue not found: " + e.workspaceID
}

func newFakeLoader(issues ...types.Issue) *fakeDepLoader {
	f := &fakeDepLoader{issues: map[string]map[int]types.Issue{}}
	for _, iss := range issues {
		if f.issues[iss.WorkspaceID] == nil {
			f.issues[iss.WorkspaceID] = map[int]types.Issue{}
		}
		f.issues[iss.WorkspaceID][iss.Number] = iss
	}
	return f
}

func TestIsCrossDepResolvedNilStore(t *testing.T) {
	dep := types.IssueDep{WorkspaceID: "ws-1", Number: 1}
	_, err := IsCrossDepResolved(nil, dep)
	if err == nil {
		t.Fatal("expected error for nil store, got nil")
	}
}

func TestIsCrossDepResolvedDoneColumn(t *testing.T) {
	loader := newFakeLoader(types.Issue{
		WorkspaceID: "ws-a",
		Number:      5,
		Column:      types.ColDone,
	})
	dep := types.IssueDep{WorkspaceID: "ws-a", Number: 5}
	ok, err := IsCrossDepResolved(loader, dep)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !ok {
		t.Error("done-column issue should be resolved")
	}
}

func TestIsCrossDepResolvedOpenIssue(t *testing.T) {
	loader := newFakeLoader(types.Issue{
		WorkspaceID: "ws-a",
		Number:      5,
		Column:      types.ColInProgress,
	})
	dep := types.IssueDep{WorkspaceID: "ws-a", Number: 5}
	ok, err := IsCrossDepResolved(loader, dep)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ok {
		t.Error("in-progress issue should NOT be resolved")
	}
}

func TestIsCrossDepResolvedClosedAt(t *testing.T) {
	now := time.Now()
	loader := newFakeLoader(types.Issue{
		WorkspaceID: "ws-b",
		Number:      3,
		Column:      types.ColReview,
		ClosedAt:    &now,
	})
	dep := types.IssueDep{WorkspaceID: "ws-b", Number: 3}
	ok, err := IsCrossDepResolved(loader, dep)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !ok {
		t.Error("closed issue (ClosedAt set) should be resolved")
	}
}

func TestIsCrossDepResolvedMissingIssue(t *testing.T) {
	loader := newFakeLoader() // empty
	dep := types.IssueDep{WorkspaceID: "ws-x", Number: 99}
	ok, err := IsCrossDepResolved(loader, dep)
	if err == nil {
		t.Fatal("expected error for missing issue, got nil")
	}
	if ok {
		t.Error("missing issue should not be resolved")
	}
}

func TestAllCrossDepsResolvedEmpty(t *testing.T) {
	loader := newFakeLoader()
	ok, err := AllCrossDepsResolved(loader, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !ok {
		t.Error("empty dep list should be resolved")
	}
}

func TestAllCrossDepsResolvedAllDone(t *testing.T) {
	loader := newFakeLoader(
		types.Issue{WorkspaceID: "ws-1", Number: 10, Column: types.ColDone},
		types.Issue{WorkspaceID: "ws-2", Number: 20, Column: types.ColDone},
	)
	deps := []types.IssueDep{
		{WorkspaceID: "ws-1", Number: 10},
		{WorkspaceID: "ws-2", Number: 20},
	}
	ok, err := AllCrossDepsResolved(loader, deps)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !ok {
		t.Error("all done deps should be resolved")
	}
}

func TestAllCrossDepsResolvedOnePending(t *testing.T) {
	loader := newFakeLoader(
		types.Issue{WorkspaceID: "ws-1", Number: 10, Column: types.ColDone},
		types.Issue{WorkspaceID: "ws-2", Number: 20, Column: types.ColTodo},
	)
	deps := []types.IssueDep{
		{WorkspaceID: "ws-1", Number: 10},
		{WorkspaceID: "ws-2", Number: 20},
	}
	ok, err := AllCrossDepsResolved(loader, deps)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ok {
		t.Error("one pending dep should make AllCrossDepsResolved return false")
	}
}

func TestPickNextUnblockedSkipsCrossDeps(t *testing.T) {
	loader := newFakeLoader(
		// dep is still in-progress (not done)
		types.Issue{WorkspaceID: "ws-other", Number: 5, Column: types.ColInProgress},
	)
	candidates := []types.Issue{
		{
			WorkspaceID: "ws-1",
			Number:      1,
			Column:      types.ColTodo,
			State:       "open",
			CrossDeps:   []types.IssueDep{{WorkspaceID: "ws-other", Number: 5}},
		},
	}
	got := pickNextUnblocked(candidates, candidates, loader)
	if got != nil {
		t.Error("issue with unresolved cross-dep should be skipped by pickNextUnblocked")
	}
}

func TestPickNextUnblockedAllowsResolvedCrossDeps(t *testing.T) {
	loader := newFakeLoader(
		types.Issue{WorkspaceID: "ws-other", Number: 5, Column: types.ColDone},
	)
	candidates := []types.Issue{
		{
			WorkspaceID: "ws-1",
			Number:      1,
			Column:      types.ColTodo,
			State:       "open",
			CrossDeps:   []types.IssueDep{{WorkspaceID: "ws-other", Number: 5}},
		},
	}
	got := pickNextUnblocked(candidates, candidates, loader)
	if got == nil {
		t.Error("issue with resolved cross-dep should be returned by pickNextUnblocked")
	}
}

func TestPickNextUnblockedNilStore(t *testing.T) {
	candidates := []types.Issue{
		{
			WorkspaceID: "ws-1",
			Number:      1,
			Column:      types.ColTodo,
			State:       "open",
			CrossDeps:   []types.IssueDep{{WorkspaceID: "ws-other", Number: 5}},
		},
	}
	// nil store: cross-dep check is skipped entirely, so issue is picked.
	got := pickNextUnblocked(candidates, candidates, nil)
	if got == nil {
		t.Error("nil store should skip cross-dep check; issue should be returned")
	}
}
