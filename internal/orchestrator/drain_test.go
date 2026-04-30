package orchestrator

import (
	"testing"
	"time"

	"prismconductor/internal/eventbus"
	"prismconductor/internal/types"
)

// pendingStubStore extends stubStore with pending-pool tracking.
type pendingStubStore struct {
	stubStore
	enqueued []types.PendingPoolRequest
	dequeued []int64
	deleted  []int
}

func (s *pendingStubStore) EnqueuePendingPool(wsID string, issueNumber int, role types.Role, action string) error {
	s.enqueued = append(s.enqueued, types.PendingPoolRequest{
		ID:          int64(len(s.enqueued) + 1),
		WorkspaceID: wsID,
		IssueNumber: issueNumber,
		Role:        role,
		Action:      action,
		CreatedAt:   time.Now(),
	})
	return nil
}
func (s *pendingStubStore) ListPendingPools(limit int) ([]types.PendingPoolRequest, error) {
	return s.enqueued, nil
}
func (s *pendingStubStore) DequeuePendingPool(id int64) error {
	s.dequeued = append(s.dequeued, id)
	out := s.enqueued[:0]
	for _, r := range s.enqueued {
		if r.ID != id {
			out = append(out, r)
		}
	}
	s.enqueued = out
	return nil
}
func (s *pendingStubStore) DeletePendingForIssue(wsID string, num int) error {
	s.deleted = append(s.deleted, num)
	out := s.enqueued[:0]
	for _, r := range s.enqueued {
		if r.WorkspaceID != wsID || r.IssueNumber != num {
			out = append(out, r)
		}
	}
	s.enqueued = out
	return nil
}
func (s *pendingStubStore) LoadIssue(wsID string, num int) (types.Issue, error) {
	return s.stubStore.LoadIssue(wsID, num)
}

// TestAutoPullEnqueuesPendingWhenPoolFull verifies that autoPull persists a
// pending-pool request when no plan slot is available instead of silently
// returning (#40).
func TestAutoPullEnqueuesPendingWhenPoolFull(t *testing.T) {
	st := &pendingStubStore{}
	st.goals = []types.Goal{
		{ID: "g1", WorkspaceID: "ws1", Status: types.GoalActive, Title: "t"},
	}
	st.issues = []types.Issue{
		{WorkspaceID: "ws1", Number: 1, State: "open", Column: types.ColTodo},
	}
	pool := &stubPool{free: 0} // pool is full
	spawnHits := 0
	spawn := func(string, int, string) error { spawnHits++; return nil }

	o := New(eventbus.New(), nil)
	o.SetStore(st)
	o.SetAutoPull(pool, spawn)

	o.autoPull("test")

	if spawnHits != 0 {
		t.Errorf("spawn called %d times on full pool, want 0", spawnHits)
	}
	if len(st.enqueued) != 1 {
		t.Errorf("enqueued %d pending requests, want 1", len(st.enqueued))
	}
	if st.enqueued[0].IssueNumber != 1 {
		t.Errorf("enqueued issue #%d, want #1", st.enqueued[0].IssueNumber)
	}
	if st.enqueued[0].Role != types.RolePlan {
		t.Errorf("enqueued role %q, want plan", st.enqueued[0].Role)
	}
}

// TestDrainPendingPoolsSpawns verifies that drainPendingPools fires the spawn
// callback when a pending request exists and the pool has a free slot.
func TestDrainPendingPoolsSpawns(t *testing.T) {
	st := &pendingStubStore{}
	st.issues = []types.Issue{
		{WorkspaceID: "ws1", Number: 5, State: "open", Column: types.ColTodo, WaitingForPool: true},
	}
	// Pre-populate with a pending request.
	_ = st.EnqueuePendingPool("ws1", 5, types.RolePlan, "spawn:plan")

	pool := &stubPool{free: 1}
	spawnHits := 0
	spawn := func(wsID string, num int, poolID string) error {
		spawnHits++
		return nil
	}

	o := New(eventbus.New(), nil)
	o.SetStore(st)
	o.SetAutoPull(pool, spawn)
	o.drainPendingPools("test")

	if spawnHits != 1 {
		t.Errorf("drain spawn called %d times, want 1", spawnHits)
	}
	if len(st.enqueued) != 0 {
		t.Errorf("pending queue has %d rows after drain, want 0", len(st.enqueued))
	}
}

// TestDrainNoop verifies that drain does nothing when no pending rows exist.
func TestDrainNoop(t *testing.T) {
	st := &pendingStubStore{}
	pool := &stubPool{free: 1}
	spawnHits := 0
	spawn := func(string, int, string) error { spawnHits++; return nil }

	o := New(eventbus.New(), nil)
	o.SetStore(st)
	o.SetAutoPull(pool, spawn)
	o.drainPendingPools("test")

	if spawnHits != 0 {
		t.Errorf("drain spawn called %d times on empty queue, want 0", spawnHits)
	}
}
