package orchestrator

import (
	"context"
	"testing"

	"prismconductor/internal/eventbus"
	"prismconductor/internal/types"
)

// stubStore satisfies the Store interface; only ListGoals/ListIssues are
// reached by autoPull's happy path.
type stubStore struct {
	goals          []types.Goal
	issues         []types.Issue
	listGoalsHits  int
	listIssuesHits int
}

func (s *stubStore) ListIssues(string) ([]types.Issue, error) {
	s.listIssuesHits++
	return s.issues, nil
}
func (s *stubStore) SaveIssue(iss types.Issue) (bool, error) {
	for i := range s.issues {
		if s.issues[i].WorkspaceID == iss.WorkspaceID && s.issues[i].Number == iss.Number {
			s.issues[i] = iss
			return false, nil
		}
	}
	return false, nil
}
func (s *stubStore) ListGoals() ([]types.Goal, error) {
	s.listGoalsHits++
	return s.goals, nil
}
func (s *stubStore) GetGoal(string) (types.Goal, error) { return types.Goal{}, nil }
func (s *stubStore) DepCacheGet(string, int, string, string) (string, bool, error) {
	return "", false, nil
}
func (s *stubStore) DepCachePut(string, int, string, string, any) error    { return nil }
func (s *stubStore) MoveIssueColumn(string, int, types.BoardColumn) error { return nil }

// Issue #40 additions.
func (s *stubStore) LoadIssue(workspaceID string, number int) (types.Issue, error) {
	for _, iss := range s.issues {
		if iss.WorkspaceID == workspaceID && iss.Number == number {
			return iss, nil
		}
	}
	return types.Issue{}, nil
}
func (s *stubStore) EnqueuePendingPool(string, int, types.Role, string) error { return nil }
func (s *stubStore) ListPendingPools(int) ([]types.PendingPoolRequest, error)  { return nil, nil }
func (s *stubStore) DequeuePendingPool(int64) error                            { return nil }
func (s *stubStore) DeletePendingForIssue(string, int) error                   { return nil }
func (s *stubStore) SetIssueWaitingForPool(string, int, bool) error            { return nil }

type stubPool struct {
	free        int
	tryAcquires int
	releases    int
	freeHits    int
}

func (p *stubPool) FreePlanSlots() int { p.freeHits++; return p.free }
func (p *stubPool) AcquireForPlan(types.Workspace) (string, bool) {
	p.tryAcquires++
	if p.free <= 0 {
		return "", false
	}
	p.free--
	return "stub-pool", true
}
func (p *stubPool) AcquireForWork(types.Workspace) (string, bool) { return "", false }
func (p *stubPool) ReleaseByPool(string)                          { p.releases++; p.free++ }

func TestSetPausedToggles(t *testing.T) {
	o := New(eventbus.New(), nil)
	if o.IsPaused() {
		t.Fatal("IsPaused should default to false")
	}
	o.SetPaused(true)
	if !o.IsPaused() {
		t.Fatal("IsPaused should be true after SetPaused(true)")
	}
	o.SetPaused(false)
	if o.IsPaused() {
		t.Fatal("IsPaused should be false after SetPaused(false)")
	}
}

func TestAutoPullShortCircuitsWhenPaused(t *testing.T) {
	st := &stubStore{
		goals: []types.Goal{
			{ID: "g1", WorkspaceID: "ws1", Status: types.GoalActive, Title: "t"},
		},
		issues: []types.Issue{
			{WorkspaceID: "ws1", Number: 1, State: "open", Column: types.ColTodo},
		},
	}
	pool := &stubPool{free: 1}
	spawnHits := 0
	spawn := func(string, int, string) error { spawnHits++; return nil }

	o := New(eventbus.New(), nil)
	o.SetStore(st)
	o.SetAutoPull(pool, spawn)
	o.SetPaused(true)

	o.autoPull("test")

	if st.listGoalsHits != 0 {
		t.Errorf("paused autoPull touched store.ListGoals (hits=%d), expected short-circuit", st.listGoalsHits)
	}
	if pool.freeHits != 0 {
		t.Errorf("paused autoPull touched pool.Free (hits=%d), expected short-circuit", pool.freeHits)
	}
	if spawnHits != 0 {
		t.Errorf("paused autoPull invoked spawn (hits=%d), expected short-circuit", spawnHits)
	}
}

func TestAutoPullProceedsWhenNotPaused(t *testing.T) {
	st := &stubStore{
		goals: []types.Goal{
			{ID: "g1", WorkspaceID: "ws1", Status: types.GoalActive, Title: "t"},
		},
		issues: []types.Issue{
			{WorkspaceID: "ws1", Number: 1, State: "open", Column: types.ColTodo},
		},
	}
	pool := &stubPool{free: 1}
	spawnHits := 0
	spawn := func(string, int, string) error { spawnHits++; return nil }

	o := New(eventbus.New(), nil)
	o.SetStore(st)
	o.SetAutoPull(pool, spawn)

	o.autoPull("test")

	if st.listGoalsHits == 0 {
		t.Error("unpaused autoPull did not call ListGoals")
	}
	if spawnHits != 1 {
		t.Errorf("unpaused autoPull spawned %d times, want 1", spawnHits)
	}
}

// fakeLLM lets runRank tests inject a deterministic response without HTTP.
type fakeLLM struct {
	called bool
	resp   string
	err    error
}

func (f *fakeLLM) Generate(_ context.Context, _, _ string) (string, error) {
	f.called = true
	return f.resp, f.err
}

// TestRunRank_NoOrchestratorPool_NoOps verifies that with a nil resolver (or
// a resolver that returns nil), runRank no-ops cleanly without erroring —
// matches today's "no Ollama" degraded behavior.
func TestRunRank_NoOrchestratorPool_NoOps(t *testing.T) {
	st := &stubStore{
		goals: []types.Goal{
			{ID: "g1", WorkspaceID: "ws1", Status: types.GoalActive, Title: "t"},
		},
		issues: []types.Issue{
			{WorkspaceID: "ws1", Number: 1, State: "open", Title: "issue", Column: types.ColTodo},
		},
	}
	o := New(eventbus.New(), func() LLM { return nil })
	o.SetStore(st)
	if err := o.runRank("test"); err != nil {
		t.Fatalf("runRank with nil LLM should not error, got %v", err)
	}
}

// TestRunRank_ResolverHit invokes the LLM only when the resolver returns a
// non-nil client. Confirms the resolver is consulted on every call (not at
// construction time).
func TestRunRank_ResolverHit(t *testing.T) {
	st := &stubStore{
		goals: []types.Goal{
			{ID: "g1", WorkspaceID: "ws1", Status: types.GoalActive, Title: "t"},
		},
		issues: []types.Issue{
			{WorkspaceID: "ws1", Number: 1, State: "open", Title: "issue", Column: types.ColTodo, Body: "x"},
		},
	}
	llm := &fakeLLM{resp: `{"ordering":[1],"primitives":[1],"dependencies":[],"rationale":"r"}`}
	o := New(eventbus.New(), func() LLM { return llm })
	o.SetStore(st)
	if err := o.runRank("test"); err != nil {
		t.Fatalf("runRank: %v", err)
	}
	if !llm.called {
		t.Fatal("resolver-returned LLM was never called by runRank")
	}
}
