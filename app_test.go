package main

import (
	"errors"
	"fmt"
	"reflect"
	"testing"
	"time"

	"prismconductor/internal/eventbus"
	"prismconductor/internal/store"
	"prismconductor/internal/types"
	"prismconductor/internal/workerpool"
)

func TestPullNumberFromURL(t *testing.T) {
	cases := []struct {
		in     string
		want   int
		wantOK bool
	}{
		{"https://github.com/o/r/pull/123", 123, true},
		{"https://github.com/o/r/pull/1", 1, true},
		{"https://github.com/foo/bar/pull/42#issuecomment-1", 42, true},
		{"https://github.com/o/r/issues/9", 0, false},
		{"", 0, false},
		{"PR_OPENED:", 0, false},
	}
	for _, c := range cases {
		got, ok := pullNumberFromURL(c.in)
		if ok != c.wantOK || got != c.want {
			t.Errorf("pullNumberFromURL(%q) = (%d,%v), want (%d,%v)", c.in, got, ok, c.want, c.wantOK)
		}
	}
}

// Issue #24: AutoApplyLabelsEnabled defaults to true when the field is absent
// (nil) so legacy workspace JSON keeps the new behavior without a migration.
func TestAutoApplyLabelsEnabled(t *testing.T) {
	tt := true
	ff := false
	cases := []struct {
		name string
		ptr  *bool
		want bool
	}{
		{"nil defaults to true (legacy JSON)", nil, true},
		{"explicit true", &tt, true},
		{"explicit false disables", &ff, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			sp := types.SkillProfile{AutoApplyLabels: c.ptr}
			if got := sp.AutoApplyLabelsEnabled(); got != c.want {
				t.Fatalf("AutoApplyLabelsEnabled() = %v, want %v", got, c.want)
			}
		})
	}
}

// Issue #24 q2 A: re-plan reconciliation drops the prior axis label only,
// preserves topical/manual labels, and unions the new suggestions.
func TestReconcileAutoLabels(t *testing.T) {
	cases := []struct {
		name      string
		current   []string
		keep      []string
		priorAxis string
		want      []string
	}{
		{
			name:      "first plan: union with empty current",
			current:   nil,
			keep:      []string{"enhancement", "frontend"},
			priorAxis: "",
			want:      []string{"enhancement", "frontend"},
		},
		{
			name:      "no-op when keep already on issue",
			current:   []string{"enhancement", "frontend"},
			keep:      []string{"enhancement", "frontend"},
			priorAxis: "",
			want:      []string{"enhancement", "frontend"},
		},
		{
			name:      "first plan: preserves manual labels",
			current:   []string{"manual-tag"},
			keep:      []string{"enhancement"},
			priorAxis: "",
			want:      []string{"manual-tag", "enhancement"},
		},
		{
			name:      "re-plan axis swap: drops prior axis, keeps topical+manual, adds new axis",
			current:   []string{"enhancement", "frontend", "manual-tag"},
			keep:      []string{"bug"},
			priorAxis: "enhancement",
			want:      []string{"frontend", "manual-tag", "bug"},
		},
		{
			name:      "re-plan: prior axis still suggested, do not drop",
			current:   []string{"enhancement", "frontend"},
			keep:      []string{"enhancement", "backend"},
			priorAxis: "enhancement",
			want:      []string{"enhancement", "frontend", "backend"},
		},
		{
			name:      "re-plan: prior axis missing from issue (already removed manually) is a no-op for drop",
			current:   []string{"frontend"},
			keep:      []string{"bug"},
			priorAxis: "enhancement",
			want:      []string{"frontend", "bug"},
		},
		{
			name:      "duplicate suggestions and labels are deduped",
			current:   []string{"frontend", "frontend"},
			keep:      []string{"frontend", "bug", "bug"},
			priorAxis: "",
			want:      []string{"frontend", "bug"},
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := reconcileAutoLabels(c.current, c.keep, c.priorAxis)
			if !reflect.DeepEqual(got, c.want) {
				t.Fatalf("reconcileAutoLabels(%v, %v, %q)\n  got:  %v\n  want: %v",
					c.current, c.keep, c.priorAxis, got, c.want)
			}
		})
	}
}

// Issue #49: pin the Archive-DONE chain end-to-end at the App layer. The bug
// report claimed the 📦 Archive N button was dead; rev1's static walk showed
// every link wired correctly. This test fails loudly if the SQL filter, the
// bus publish, or the ListIssues exclusion ever regress.
func TestApp_ArchiveDone_PublishesEventAndExcludesFromListIssues(t *testing.T) {
	s, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatalf("store.Open: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })

	bus := eventbus.New()
	a := &App{store: s, bus: bus}

	seed := func(num int, col types.BoardColumn) {
		if _, err := s.SaveIssue(types.Issue{
			WorkspaceID: "ws1",
			Number:      num,
			Title:       fmt.Sprintf("#%d", num),
			State:       "open",
			Column:      col,
		}); err != nil {
			t.Fatalf("SaveIssue %d: %v", num, err)
		}
	}
	seed(101, types.ColDone)
	seed(102, types.ColDone)
	seed(103, types.ColDone)
	seed(200, types.ColInProgress)

	captured := make(chan eventbus.Event, 4)
	bus.Subscribe(func(e eventbus.Event) {
		if e.Type == eventbus.EvtIssuesArchived {
			captured <- e
		}
	})

	n, err := a.ArchiveDone("ws1")
	if err != nil {
		t.Fatalf("ArchiveDone: %v", err)
	}
	if n != 3 {
		t.Errorf("count = %d, want 3", n)
	}

	select {
	case evt := <-captured:
		p, ok := evt.Payload.(map[string]any)
		if !ok {
			t.Fatalf("payload type = %T, want map[string]any", evt.Payload)
		}
		if p["workspace_id"] != "ws1" {
			t.Errorf("workspace_id = %v, want ws1", p["workspace_id"])
		}
		if p["count"] != 3 {
			t.Errorf("count = %v, want 3", p["count"])
		}
	case <-time.After(time.Second):
		t.Fatal("EvtIssuesArchived not published within 1s")
	}

	live, err := a.ListIssues("ws1")
	if err != nil {
		t.Fatalf("ListIssues: %v", err)
	}
	if len(live) != 1 || live[0].Number != 200 {
		t.Errorf("ListIssues after archive = %v, want only #200", live)
	}

	arch, err := a.ListArchivedIssues("ws1")
	if err != nil {
		t.Fatalf("ListArchivedIssues: %v", err)
	}
	if len(arch) != 3 {
		t.Errorf("archived count = %d, want 3", len(arch))
	}
}

// FetchIssueDetail falls back to the local mirror when the github client is unavailable.
func TestApp_FetchIssueDetail_FallbackToLocal(t *testing.T) {
	s, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatalf("store.Open: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })

	if _, err := s.SaveIssue(types.Issue{
		WorkspaceID: "ws1",
		Number:      42,
		Title:       "test issue",
		Body:        "body text",
		State:       "open",
		Column:      types.ColTodo,
	}); err != nil {
		t.Fatalf("SaveIssue: %v", err)
	}

	a := &App{store: s, gh: nil}
	iss, err := a.FetchIssueDetail("ws1", 42)
	if err != nil {
		t.Fatalf("FetchIssueDetail: %v", err)
	}
	if iss.Number != 42 || iss.Body != "body text" {
		t.Errorf("unexpected issue: %+v", iss)
	}
}

// FetchIssueDetail returns a cached result on the second call without hitting GitHub.
func TestApp_FetchIssueDetail_Cache(t *testing.T) {
	s, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatalf("store.Open: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })

	a := &App{store: s, gh: nil}
	cached := types.Issue{WorkspaceID: "ws1", Number: 7, Title: "cached", Body: "cached body", State: "open"}
	a.issueDetailCache.Store("ws1#7", issueDetailEntry{issue: cached, expiresAt: time.Now().Add(60 * time.Second)})

	iss, err := a.FetchIssueDetail("ws1", 7)
	if err != nil {
		t.Fatalf("FetchIssueDetail: %v", err)
	}
	if iss.Title != "cached" {
		t.Errorf("expected cached title, got %q", iss.Title)
	}
}

func TestLabelSetEqual(t *testing.T) {
	cases := []struct {
		a, b []string
		want bool
	}{
		{nil, nil, true},
		{[]string{}, nil, true},
		{[]string{"a"}, []string{"a"}, true},
		{[]string{"a", "b"}, []string{"b", "a"}, true},
		{[]string{"a"}, []string{"a", "b"}, false},
		{[]string{"a", "b"}, []string{"a", "c"}, false},
	}
	for _, c := range cases {
		if got := labelSetEqual(c.a, c.b); got != c.want {
			t.Errorf("labelSetEqual(%v, %v) = %v, want %v", c.a, c.b, got, c.want)
		}
	}
}

// handleSpawnExecuteFailure rolls back the column to PLAN, sets failure_reason,
// and releases the pool slot (issue #218).
func TestHandleSpawnExecuteFailure_RollsBackAndSetsReason(t *testing.T) {
	s, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatalf("store.Open: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })

	const wsID = "ws1"
	const issueNum = 42
	if _, err := s.SaveIssue(types.Issue{
		WorkspaceID: wsID,
		Number:      issueNum,
		Title:       "test issue",
		State:       "open",
		Column:      types.ColInProgress,
	}); err != nil {
		t.Fatalf("SaveIssue: %v", err)
	}

	const poolID = "p1"
	reg := workerpool.NewRegistry(func(types.Provider) bool { return true })
	reg.Sync([]types.Pool{{ID: poolID, Capacity: 1, Enabled: true, Role: types.RoleWork}})
	// Simulate one slot already acquired (the one ApprovePlan took before failing).
	if _, ok := reg.AcquireForWork(types.Workspace{ID: wsID}); !ok {
		t.Fatal("AcquireForWork failed — pool not set up correctly")
	}
	if reg.FreeWorkSlots() != 0 {
		t.Fatalf("expected 0 free slots after acquire, got %d", reg.FreeWorkSlots())
	}

	a := &App{store: s, poolReg: reg, bus: eventbus.New()}
	// Pass a minimal pool stub — only pool.ID is used by handleSpawnExecuteFailure.
	a.handleSpawnExecuteFailure(wsID, issueNum, types.Pool{ID: poolID}, errors.New("no such binary"))

	// Pool slot must be released.
	if reg.FreeWorkSlots() != 1 {
		t.Errorf("expected 1 free slot after failure, got %d", reg.FreeWorkSlots())
	}

	// Column must be rolled back to PLAN.
	iss, err := s.LoadIssue(wsID, issueNum)
	if err != nil {
		t.Fatalf("LoadIssue: %v", err)
	}
	if iss.Column != types.ColPlan {
		t.Errorf("column = %q, want %q", iss.Column, types.ColPlan)
	}

	// failure_reason must be set.
	if iss.FailureReason == "" {
		t.Error("failure_reason not set after spawn failure")
	}
}

// RetryExecuteForApprovedPlan returns an error when store/registry unavailable.
func TestRetryExecuteForApprovedPlan_RequiresRegistry(t *testing.T) {
	a := &App{bus: eventbus.New()}
	err := a.RetryExecuteForApprovedPlan("ws1", 1)
	if err == nil {
		t.Fatal("expected error when wsReg and store are nil")
	}
}

// Issue #247: when an execute session fails after a PR has been opened,
// handleSessionStateChange must move the card to REVIEW and emit a toast.
func TestHandleSessionStateChange_RollsBackToReviewWhenPROpen(t *testing.T) {
	s, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatalf("store.Open: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })

	const wsID = "ws1"
	const issueNum = 77
	prNum := 99
	if _, err := s.SaveIssue(types.Issue{
		WorkspaceID: wsID,
		Number:      issueNum,
		Title:       "issue with open PR",
		State:       "open",
		Column:      types.ColInProgress,
		PRNumber:    &prNum,
	}); err != nil {
		t.Fatalf("SaveIssue: %v", err)
	}

	a := &App{store: s, bus: eventbus.New()}
	failedSess := types.Session{
		ID:            "sess-fail",
		WorkspaceID:   wsID,
		IssueNumber:   issueNum,
		Mode:          types.ModeExecute,
		State:         types.StateBlocked,
		BlockedReason: "lint failed",
	}
	a.handleSessionStateChange(failedSess, types.StateRunning)

	// Card must be rolled back to REVIEW.
	iss, err := s.LoadIssue(wsID, issueNum)
	if err != nil {
		t.Fatalf("LoadIssue: %v", err)
	}
	if iss.Column != types.ColReview {
		t.Errorf("column = %q, want %q", iss.Column, types.ColReview)
	}
}

// Issue #247: when an execute session fails with NO open PR, the card must
// NOT be rolled back to REVIEW (preserve existing behaviour).
func TestHandleSessionStateChange_NoRollbackWhenNoPR(t *testing.T) {
	s, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatalf("store.Open: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })

	const wsID = "ws1"
	const issueNum = 88
	if _, err := s.SaveIssue(types.Issue{
		WorkspaceID: wsID,
		Number:      issueNum,
		Title:       "issue without PR",
		State:       "open",
		Column:      types.ColInProgress,
	}); err != nil {
		t.Fatalf("SaveIssue: %v", err)
	}

	a := &App{store: s, bus: eventbus.New()}
	failedSess := types.Session{
		ID:            "sess-fail-no-pr",
		WorkspaceID:   wsID,
		IssueNumber:   issueNum,
		Mode:          types.ModeExecute,
		State:         types.StateFailed,
		BlockedReason: "build failed",
	}
	a.handleSessionStateChange(failedSess, types.StateRunning)

	// Column must NOT have changed — no PR, no rollback.
	iss, err := s.LoadIssue(wsID, issueNum)
	if err != nil {
		t.Fatalf("LoadIssue: %v", err)
	}
	if iss.Column != types.ColInProgress {
		t.Errorf("column = %q, want %q (no rollback expected without PR)", iss.Column, types.ColInProgress)
	}
}
