package orchestrator

import (
	"testing"

	"prismconductor/internal/eventbus"
	"prismconductor/internal/types"
)

// stubStore satisfies the Store interface; only ListGoals/ListIssues are
// reached by autoPull's happy path.
type stubStore struct {
	goals       []types.Goal
	issues      []types.Issue
	listGoalsHits  int
	listIssuesHits int
}

func (s *stubStore) ListIssues(string) ([]types.Issue, error) {
	s.listIssuesHits++
	return s.issues, nil
}
func (s *stubStore) SaveIssue(types.Issue) (bool, error) { return false, nil }
func (s *stubStore) ListGoals() ([]types.Goal, error) {
	s.listGoalsHits++
	return s.goals, nil
}
func (s *stubStore) GetGoal(string) (types.Goal, error)             { return types.Goal{}, nil }
func (s *stubStore) DepCacheGet(string, int, string, string) (string, bool, error) {
	return "", false, nil
}
func (s *stubStore) DepCachePut(string, int, string, string, any) error { return nil }
func (s *stubStore) MoveIssueColumn(string, int, types.BoardColumn) error { return nil }

type stubPool struct {
	free        int
	tryAcquires int
	releases    int
	freeHits    int
}

func (p *stubPool) FreeForSpawn() int { p.freeHits++; return p.free }
func (p *stubPool) AcquireFor(types.Workspace) (string, bool) {
	p.tryAcquires++
	if p.free <= 0 {
		return "", false
	}
	p.free--
	return "stub-pool", true
}
func (p *stubPool) ReleaseByPool(string) { p.releases++; p.free++ }

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
