package diagnose

import (
	"testing"
	"time"

	"prismconductor/internal/types"
)

func TestRun_liveRunning(t *testing.T) {
	sess := types.Session{
		ID:          "abc12345",
		WorkspaceID: "ws1",
		IssueNumber: 42,
		Mode:        types.ModeExecute,
		State:       types.StateRunning,
		StartedAt:   time.Now().Add(-5 * time.Minute),
	}
	s := Snapshot{
		Issue:       types.Issue{Number: 42, Column: types.ColInProgress},
		LiveSession: &sess,
		PoolName:    "work-pool",
		PoolActive:  1,
		PoolCapacity: 2,
	}
	d := Run(s)
	if d.SeverityLevel != "ok" {
		t.Errorf("expected ok, got %s", d.SeverityLevel)
	}
	if len(d.Detail) == 0 {
		t.Error("expected non-empty detail")
	}
}

func TestRun_liveBlocked(t *testing.T) {
	sess := types.Session{
		ID:            "abc12345",
		WorkspaceID:   "ws1",
		IssueNumber:   42,
		Mode:          types.ModeExecute,
		State:         types.StateBlocked,
		StartedAt:     time.Now().Add(-10 * time.Minute),
		BlockedReason: "tests failed — 3 failures",
	}
	s := Snapshot{
		Issue:       types.Issue{Number: 42, Column: types.ColInProgress},
		LiveSession: &sess,
	}
	d := Run(s)
	if d.SeverityLevel != "stuck" {
		t.Errorf("expected stuck, got %s", d.SeverityLevel)
	}
	found := false
	for _, line := range d.Detail {
		if line == "Blocked reason: tests failed — 3 failures" {
			found = true
		}
	}
	if !found {
		t.Errorf("blocked reason not in detail: %v", d.Detail)
	}
}

func TestRun_livePausedForQuestion(t *testing.T) {
	sess := types.Session{
		ID:                "abc12345",
		WorkspaceID:       "ws1",
		IssueNumber:       42,
		Mode:              types.ModeExecute,
		State:             types.StatePausedForQuestion,
		PendingQuestionID: "q-uuid-1",
		StartedAt:         time.Now().Add(-2 * time.Minute),
	}
	s := Snapshot{
		Issue:       types.Issue{Number: 42, Column: types.ColInProgress},
		LiveSession: &sess,
	}
	d := Run(s)
	if d.SeverityLevel != "warn" {
		t.Errorf("expected warn, got %s", d.SeverityLevel)
	}
}

func TestRun_pendingPool(t *testing.T) {
	s := Snapshot{
		Issue:       types.Issue{Number: 5, Column: types.ColInProgress},
		PendingPool: true,
		PoolName:    "work-pool",
		PoolActive:  2,
		PoolCapacity: 2,
	}
	d := Run(s)
	if d.SeverityLevel != "warn" {
		t.Errorf("expected warn, got %s", d.SeverityLevel)
	}
}

func TestRun_waitingForPoolFlag(t *testing.T) {
	s := Snapshot{
		Issue: types.Issue{
			Number:         5,
			Column:         types.ColInProgress,
			WaitingForPool: true,
		},
	}
	d := Run(s)
	if d.SeverityLevel != "warn" {
		t.Errorf("expected warn, got %s", d.SeverityLevel)
	}
}

func TestRun_lastSessionBlocked(t *testing.T) {
	endAt := time.Now().Add(-12 * time.Minute)
	sess := types.Session{
		ID:            "zz",
		Mode:          types.ModeExecute,
		State:         types.StateBlocked,
		BlockedReason: "harness input-token cap reached",
		EndedAt:       &endAt,
	}
	s := Snapshot{
		Issue:    types.Issue{Number: 47, Column: types.ColInProgress},
		Sessions: []types.Session{sess},
	}
	d := Run(s)
	if d.SeverityLevel != "stuck" {
		t.Errorf("expected stuck, got %s", d.SeverityLevel)
	}
}

func TestRun_lastSessionCompleted(t *testing.T) {
	endAt := time.Now().Add(-3 * time.Minute)
	sess := types.Session{
		Mode:    types.ModePlan,
		State:   types.StateCompleted,
		EndedAt: &endAt,
	}
	s := Snapshot{
		Issue:    types.Issue{Number: 10, Column: types.ColReview},
		Sessions: []types.Session{sess},
	}
	d := Run(s)
	if d.SeverityLevel != "ok" {
		t.Errorf("expected ok, got %s", d.SeverityLevel)
	}
}

func TestRun_planOnDiskButNotIngested(t *testing.T) {
	s := Snapshot{
		Issue:        types.Issue{Number: 99, Column: types.ColTodo},
		PlanOnDisk:   true,
		PlanRevision: 2,
	}
	d := Run(s)
	if d.SeverityLevel != "stuck" {
		t.Errorf("expected stuck, got %s", d.SeverityLevel)
	}
}

func TestRun_idleInPlanColumn(t *testing.T) {
	s := Snapshot{
		Issue: types.Issue{Number: 7, Column: types.ColPlan},
	}
	d := Run(s)
	if d.SeverityLevel != "warn" {
		t.Errorf("expected warn, got %s", d.SeverityLevel)
	}
}

func TestRun_idleTodo(t *testing.T) {
	s := Snapshot{
		Issue: types.Issue{Number: 8, Column: types.ColTodo},
	}
	d := Run(s)
	if d.SeverityLevel != "ok" {
		t.Errorf("expected ok, got %s", d.SeverityLevel)
	}
}

func TestFmtElapsed(t *testing.T) {
	cases := []struct {
		in  time.Duration
		out string
	}{
		{30 * time.Second, "30s"},
		{90 * time.Second, "1m30s"},
		{2 * time.Minute, "2m"},
	}
	for _, c := range cases {
		if got := fmtElapsed(c.in); got != c.out {
			t.Errorf("fmtElapsed(%v) = %q, want %q", c.in, got, c.out)
		}
	}
}
