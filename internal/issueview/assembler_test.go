package issueview

import (
	"fmt"
	"testing"
	"time"

	"prismconductor/internal/eventbus"
	"prismconductor/internal/types"
)

// fakeStore is a minimal issueStore for Assembler integration tests.
type fakeStore struct {
	issues   map[int]types.Issue
	sessions map[int][]types.Session
}

func (f *fakeStore) LoadIssue(_ string, number int) (types.Issue, error) {
	if iss, ok := f.issues[number]; ok {
		return iss, nil
	}
	return types.Issue{Number: number}, nil
}

func (f *fakeStore) ListIssues(_ string) ([]types.Issue, error) {
	out := make([]types.Issue, 0, len(f.issues))
	for _, iss := range f.issues {
		out = append(out, iss)
	}
	return out, nil
}

func (f *fakeStore) LatestPlan(_ string, _ int) (*types.Plan, error) { return nil, nil }

func (f *fakeStore) ListSessionsForIssue(_ string, issueNumber int) ([]types.Session, error) {
	return f.sessions[issueNumber], nil
}

func (f *fakeStore) GetPool(id string) (types.Pool, error) {
	return types.Pool{}, fmt.Errorf("pool not found")
}

// --- helpers ---

func makeSession(state types.SessionState, startedAt time.Time, blockedReason string, acknowledged bool) types.Session {
	sess := types.Session{
		ID:            "sess-" + string(state) + "-" + startedAt.Format("150405"),
		State:         state,
		StartedAt:     startedAt,
		BlockedReason: blockedReason,
	}
	if acknowledged {
		v := int64(1)
		sess.AcknowledgedAt = &v
	}
	return sess
}

func makeEvent(t eventbus.EventType, payload any) eventbus.Event {
	return eventbus.Event{Type: t, Payload: payload}
}

func planPtr(readyToExecute bool, approved bool) *types.Plan {
	p := &types.Plan{Revision: 1, ReadyToExecute: readyToExecute}
	if approved {
		now := time.Now()
		p.ApprovedAt = &now
	}
	return p
}

func prNumber(n int) *int { return &n }

var (
	t0 = time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	t1 = t0.Add(time.Hour)
	t2 = t0.Add(2 * time.Hour)
	t3 = t0.Add(3 * time.Hour)
)

// --- selectSessions ---

func TestSelectSessions_ActiveRunning(t *testing.T) {
	sessions := []types.Session{makeSession(types.StateRunning, t1, "", false)}
	iss := types.Issue{Column: types.ColInProgress}
	active, paused, lastFail, _ := selectSessions(iss, sessions)
	if active == nil || active.State != types.StateRunning {
		t.Fatalf("expected active running, got %v", active)
	}
	if paused != nil || lastFail != nil {
		t.Errorf("expected nil paused/lastFail")
	}
}

func TestSelectSessions_PausedTracked(t *testing.T) {
	s := makeSession(types.StatePausedForQuestion, t1, "", false)
	s.PendingQuestionID = "q1"
	iss := types.Issue{Column: types.ColInProgress}
	_, paused, _, _ := selectSessions(iss, []types.Session{s})
	if paused == nil || paused.State != types.StatePausedForQuestion {
		t.Fatal("expected paused session")
	}
}

func TestSelectSessions_LastFailureTracked(t *testing.T) {
	sessions := []types.Session{makeSession(types.StateFailed, t1, "BLOCKED: reason", false)}
	_, _, lastFail, _ := selectSessions(types.Issue{Column: types.ColTodo}, sessions)
	if lastFail == nil || lastFail.BlockedReason != "BLOCKED: reason" {
		t.Fatalf("expected lastFail with reason, got %v", lastFail)
	}
}

func TestSelectSessions_LastFailureSuppressedInReview(t *testing.T) {
	sessions := []types.Session{makeSession(types.StateFailed, t1, "BLOCKED: x", false)}
	_, _, lastFail, _ := selectSessions(types.Issue{Column: types.ColReview}, sessions)
	if lastFail != nil {
		t.Error("lastFail should be suppressed in review")
	}
}

func TestSelectSessions_LastFailureSuppressedInDone(t *testing.T) {
	sessions := []types.Session{makeSession(types.StateFailed, t1, "BLOCKED: x", false)}
	_, _, lastFail, _ := selectSessions(types.Issue{Column: types.ColDone}, sessions)
	if lastFail != nil {
		t.Error("lastFail should be suppressed in done")
	}
}

func TestSelectSessions_LastFailureSuppressedByLaterSuccess(t *testing.T) {
	sessions := []types.Session{
		makeSession(types.StateCompleted, t3, "", false),
		makeSession(types.StateFailed, t1, "BLOCKED: old", false),
	}
	_, _, lastFail, _ := selectSessions(types.Issue{Column: types.ColPlan}, sessions)
	if lastFail != nil {
		t.Error("lastFail should be suppressed by later completed session")
	}
}

func TestSelectSessions_AcknowledgedFailureIgnored(t *testing.T) {
	sessions := []types.Session{makeSession(types.StateFailed, t1, "BLOCKED: acked", true)}
	_, _, lastFail, _ := selectSessions(types.Issue{Column: types.ColTodo}, sessions)
	if lastFail != nil {
		t.Error("acknowledged failure should not surface as lastFail")
	}
}

func TestSelectSessions_MostRecentFailureWins(t *testing.T) {
	sessions := []types.Session{
		makeSession(types.StateFailed, t2, "newer", false),
		makeSession(types.StateFailed, t1, "older", false),
	}
	_, _, lastFail, _ := selectSessions(types.Issue{Column: types.ColTodo}, sessions)
	if lastFail == nil || lastFail.BlockedReason != "newer" {
		t.Errorf("expected newer reason, got %v", lastFail)
	}
}

func TestSelectSessions_NoReason_NotTrackedAsFailure(t *testing.T) {
	sessions := []types.Session{makeSession(types.StateFailed, t1, "", false)}
	_, _, lastFail, _ := selectSessions(types.Issue{Column: types.ColTodo}, sessions)
	if lastFail != nil {
		t.Error("failed session with no reason should not surface as lastFail")
	}
}

// --- derivedColumn ---

func TestDerivedColumn_PRNumber_TrustsReview(t *testing.T) {
	iss := types.Issue{Column: types.ColReview, PRNumber: prNumber(42)}
	if col := derivedColumn(iss, nil, nil); col != types.ColReview {
		t.Errorf("got %v want review", col)
	}
}

func TestDerivedColumn_PRNumber_TrustsDone(t *testing.T) {
	iss := types.Issue{Column: types.ColDone, PRNumber: prNumber(42)}
	if col := derivedColumn(iss, nil, nil); col != types.ColDone {
		t.Errorf("got %v want done", col)
	}
}

func TestDerivedColumn_ActiveSession_InProgress(t *testing.T) {
	iss := types.Issue{Column: types.ColTodo}
	sess := makeSession(types.StateRunning, t1, "", false)
	if col := derivedColumn(iss, nil, &sess); col != types.ColInProgress {
		t.Errorf("got %v want in_progress", col)
	}
}

func TestDerivedColumn_PlanReady(t *testing.T) {
	iss := types.Issue{Column: types.ColTodo}
	if col := derivedColumn(iss, planPtr(true, false), nil); col != types.ColPlan {
		t.Errorf("got %v want plan", col)
	}
}

func TestDerivedColumn_PlanApproved_NoOverride(t *testing.T) {
	iss := types.Issue{Column: types.ColTodo}
	if col := derivedColumn(iss, planPtr(true, true), nil); col != types.ColTodo {
		t.Errorf("got %v want todo (approved plan should not force plan column)", col)
	}
}

func TestDerivedColumn_Fallback_EmptyColumn(t *testing.T) {
	if col := derivedColumn(types.Issue{}, nil, nil); col != types.ColTodo {
		t.Errorf("got %v want todo", col)
	}
}

func TestDerivedColumn_Fallback_StoredColumn(t *testing.T) {
	iss := types.Issue{Column: types.ColInProgress}
	if col := derivedColumn(iss, nil, nil); col != types.ColInProgress {
		t.Errorf("got %v want in_progress", col)
	}
}

// --- PRChecksFailed handling ---

func TestAssembler_CheckFailureStored(t *testing.T) {
	bus := eventbus.New()
	st := &fakeStore{issues: map[int]types.Issue{7: {Number: 7, WorkspaceID: "ws1", Column: types.ColReview}}}
	a := New(bus, st)

	bus.Publish(eventbus.EvtPRChecksFailed, eventbus.PRChecksFailed{
		WorkspaceID:         "ws1",
		IssueNumber:         7,
		PRNumber:            42,
		HeadSHA:             "abc123",
		FailingJobs:         []string{"build", "test"},
		FailingCheckRunURLs: []string{"https://gh/1", "https://gh/2"},
	})
	// Bus handlers run in goroutines; give them time to settle.
	time.Sleep(20 * time.Millisecond)

	view, err := a.Assemble("ws1", 7)
	if err != nil {
		t.Fatalf("Assemble: %v", err)
	}
	if view.TestsFailingInfo == nil {
		t.Fatal("expected TestsFailingInfo to be populated after EvtPRChecksFailed")
	}
	if len(view.TestsFailingInfo.FailingJobs) != 2 {
		t.Errorf("want 2 failing jobs, got %d", len(view.TestsFailingInfo.FailingJobs))
	}
	if view.TestsFailingInfo.HeadSHA != "abc123" {
		t.Errorf("want head_sha abc123, got %q", view.TestsFailingInfo.HeadSHA)
	}
}

func TestAssembler_CheckFailureClearedOnRecovery(t *testing.T) {
	bus := eventbus.New()
	st := &fakeStore{issues: map[int]types.Issue{8: {Number: 8, WorkspaceID: "ws1", Column: types.ColReview}}}
	a := New(bus, st)

	bus.Publish(eventbus.EvtPRChecksFailed, eventbus.PRChecksFailed{
		WorkspaceID: "ws1", IssueNumber: 8, PRNumber: 1, HeadSHA: "sha1",
		FailingJobs: []string{"lint"},
	})
	time.Sleep(20 * time.Millisecond)

	bus.Publish(eventbus.EvtPRChecksRecovered, eventbus.PRChecksRecovered{
		WorkspaceID: "ws1", IssueNumber: 8, PRNumber: 1, HeadSHA: "sha1",
	})
	time.Sleep(20 * time.Millisecond)

	view, err := a.Assemble("ws1", 8)
	if err != nil {
		t.Fatalf("Assemble: %v", err)
	}
	if view.TestsFailingInfo != nil {
		t.Errorf("expected TestsFailingInfo cleared after recovery, got %+v", view.TestsFailingInfo)
	}
}

func TestAssembler_SetSelfHealAttempts(t *testing.T) {
	bus := eventbus.New()
	st := &fakeStore{issues: map[int]types.Issue{9: {Number: 9, WorkspaceID: "ws2", Column: types.ColReview}}}
	a := New(bus, st)

	bus.Publish(eventbus.EvtPRChecksFailed, eventbus.PRChecksFailed{
		WorkspaceID: "ws2", IssueNumber: 9, PRNumber: 3, HeadSHA: "sha2",
		FailingJobs: []string{"unit-tests"},
	})
	time.Sleep(20 * time.Millisecond)

	a.SetSelfHealAttempts("ws2", 9, 2, 3, false)

	view, err := a.Assemble("ws2", 9)
	if err != nil {
		t.Fatalf("Assemble: %v", err)
	}
	if view.TestsFailingInfo == nil {
		t.Fatal("expected TestsFailingInfo present")
	}
	if view.TestsFailingInfo.SelfHealAttempts != 2 {
		t.Errorf("want 2 attempts, got %d", view.TestsFailingInfo.SelfHealAttempts)
	}
	if view.TestsFailingInfo.MaxAttemptsReached {
		t.Error("should not be max-reached at 2/3")
	}
}

func TestExtractIssueKey_PRChecksFailed(t *testing.T) {
	e := makeEvent(eventbus.EvtPRChecksFailed, eventbus.PRChecksFailed{
		WorkspaceID: "ws3",
		IssueNumber: 55,
		PRNumber:    10,
	})
	ws, num := extractIssueKey(e)
	if ws != "ws3" || num != 55 {
		t.Errorf("got ws=%q num=%d", ws, num)
	}
}

func TestExtractIssueKey_PRChecksRecovered(t *testing.T) {
	e := makeEvent(eventbus.EvtPRChecksRecovered, eventbus.PRChecksRecovered{
		WorkspaceID: "ws3",
		IssueNumber: 56,
		PRNumber:    10,
	})
	ws, num := extractIssueKey(e)
	if ws != "ws3" || num != 56 {
		t.Errorf("got ws=%q num=%d", ws, num)
	}
}

// --- extractIssueKey ---

func TestExtractIssueKey_SessionStateChanged(t *testing.T) {
	e := makeEvent(eventbus.EvtSessionStateChanged, eventbus.SessionStateChanged{
		WorkspaceID: "ws1",
		IssueNumber: 42,
		SessionID:   "s1",
	})
	ws, num := extractIssueKey(e)
	if ws != "ws1" || num != 42 {
		t.Errorf("got ws=%q num=%d", ws, num)
	}
}

func TestExtractIssueKey_MapIssueNumber_Float64(t *testing.T) {
	e := makeEvent(eventbus.EvtPlanReady, map[string]any{
		"workspace_id": "ws2",
		"issue_number": float64(7),
	})
	ws, num := extractIssueKey(e)
	if ws != "ws2" || num != 7 {
		t.Errorf("got ws=%q num=%d", ws, num)
	}
}

func TestExtractIssueKey_MapNumber_Float64(t *testing.T) {
	e := makeEvent(eventbus.EvtCardMovedManually, map[string]any{
		"workspace_id": "ws3",
		"number":       float64(99),
	})
	ws, num := extractIssueKey(e)
	if ws != "ws3" || num != 99 {
		t.Errorf("got ws=%q num=%d", ws, num)
	}
}

func TestExtractIssueKey_PendingPoolChange(t *testing.T) {
	e := makeEvent(eventbus.EvtPendingPoolEnqueued, eventbus.PendingPoolChange{
		WorkspaceID: "ws4",
		IssueNumber: 5,
		Role:        "work",
	})
	ws, num := extractIssueKey(e)
	if ws != "ws4" || num != 5 {
		t.Errorf("got ws=%q num=%d", ws, num)
	}
}

func TestExtractIssueKey_UnknownPayload_ReturnsEmpty(t *testing.T) {
	e := makeEvent(eventbus.EvtGoalUpdated, "some-goal-id")
	ws, num := extractIssueKey(e)
	if ws != "" || num != 0 {
		t.Errorf("expected empty, got ws=%q num=%d", ws, num)
	}
}

// --- PRConflictsDetected handling (#124) ---

func TestAssembler_ConflictStoredOnDetected(t *testing.T) {
	bus := eventbus.New()
	st := &fakeStore{issues: map[int]types.Issue{10: {Number: 10, WorkspaceID: "ws1", Column: types.ColReview}}}
	a := New(bus, st)

	bus.Publish(eventbus.EvtPRConflictsDetected, eventbus.PRConflictsDetected{
		WorkspaceID:      "ws1",
		IssueNumber:      10,
		PRNumber:         55,
		Base:             "main",
		Head:             "feat/foo",
		ConflictingFiles: []string{"go.sum", "internal/foo/bar.go"},
	})
	time.Sleep(20 * time.Millisecond)

	view, err := a.Assemble("ws1", 10)
	if err != nil {
		t.Fatalf("Assemble: %v", err)
	}
	if view.ConflictsInfo == nil {
		t.Fatal("expected ConflictsInfo populated after EvtPRConflictsDetected")
	}
	if view.ConflictsInfo.Base != "main" {
		t.Errorf("want base=main, got %q", view.ConflictsInfo.Base)
	}
	if len(view.ConflictsInfo.ConflictingFiles) != 2 {
		t.Errorf("want 2 conflicting files, got %d", len(view.ConflictsInfo.ConflictingFiles))
	}
}

func TestAssembler_ConflictClearedOnResolved(t *testing.T) {
	bus := eventbus.New()
	st := &fakeStore{issues: map[int]types.Issue{11: {Number: 11, WorkspaceID: "ws1", Column: types.ColReview}}}
	a := New(bus, st)

	bus.Publish(eventbus.EvtPRConflictsDetected, eventbus.PRConflictsDetected{
		WorkspaceID: "ws1", IssueNumber: 11, PRNumber: 56, Base: "main", Head: "feat/bar",
		ConflictingFiles: []string{"README.md"},
	})
	time.Sleep(20 * time.Millisecond)

	bus.Publish(eventbus.EvtPRConflictsResolved, eventbus.PRConflictsResolved{
		WorkspaceID: "ws1", IssueNumber: 11, PRNumber: 56,
	})
	time.Sleep(20 * time.Millisecond)

	view, err := a.Assemble("ws1", 11)
	if err != nil {
		t.Fatalf("Assemble: %v", err)
	}
	if view.ConflictsInfo != nil {
		t.Errorf("expected ConflictsInfo cleared after EvtPRConflictsResolved, got %+v", view.ConflictsInfo)
	}
}

func TestAssembler_ConflictClearedOnMerge(t *testing.T) {
	bus := eventbus.New()
	st := &fakeStore{issues: map[int]types.Issue{12: {Number: 12, WorkspaceID: "ws1", Column: types.ColReview}}}
	a := New(bus, st)

	bus.Publish(eventbus.EvtPRConflictsDetected, eventbus.PRConflictsDetected{
		WorkspaceID: "ws1", IssueNumber: 12, PRNumber: 57, Base: "main", Head: "feat/baz",
	})
	time.Sleep(20 * time.Millisecond)

	bus.Publish(eventbus.EvtPRMerged, map[string]any{
		"workspace_id": "ws1",
		"issue_number": 12,
	})
	time.Sleep(20 * time.Millisecond)

	view, err := a.Assemble("ws1", 12)
	if err != nil {
		t.Fatalf("Assemble: %v", err)
	}
	if view.ConflictsInfo != nil {
		t.Errorf("expected ConflictsInfo cleared after EvtPRMerged, got %+v", view.ConflictsInfo)
	}
}

func TestExtractIssueKey_PRConflictsDetected(t *testing.T) {
	e := makeEvent(eventbus.EvtPRConflictsDetected, eventbus.PRConflictsDetected{
		WorkspaceID: "ws5", IssueNumber: 77, PRNumber: 20,
	})
	ws, num := extractIssueKey(e)
	if ws != "ws5" || num != 77 {
		t.Errorf("got ws=%q num=%d", ws, num)
	}
}

func TestExtractIssueKey_PRConflictsResolved(t *testing.T) {
	e := makeEvent(eventbus.EvtPRConflictsResolved, eventbus.PRConflictsResolved{
		WorkspaceID: "ws5", IssueNumber: 78, PRNumber: 21,
	})
	ws, num := extractIssueKey(e)
	if ws != "ws5" || num != 78 {
		t.Errorf("got ws=%q num=%d", ws, num)
	}
}

// --- cross-contamination / pointer aliasing ---

// TestSelectSessions_DeepCopyPreventsAliasing verifies that two calls to
// selectSessions on the same slice return independent pointers. Before the
// deep-copy fix, both calls returned &sessions[i] which pointed into the
// same backing array, so mutating one would silently corrupt the other.
func TestSelectSessions_DeepCopyPreventsAliasing(t *testing.T) {
	sessions := []types.Session{makeSession(types.StateRunning, t1, "", false)}
	iss := types.Issue{Column: types.ColInProgress}

	active1, _, _, _ := selectSessions(iss, sessions)
	active2, _, _, _ := selectSessions(iss, sessions)
	if active1 == nil || active2 == nil {
		t.Fatal("both calls must return a non-nil active session")
	}
	active1.BlockedReason = "mutated"
	if active2.BlockedReason == "mutated" {
		t.Error("selectSessions returned aliased pointers — each returned struct must be independent")
	}
}

// TestAssemble_NoCrossIssueContamination seeds two issues with distinct
// sessions and verifies that each IssueView's ActiveSession is owned by
// the correct issue and that mutating one view's pointer doesn't bleed into
// the other view.
func TestAssemble_NoCrossIssueContamination(t *testing.T) {
	sessA := makeSession(types.StateRunning, t1, "", false)
	sessA.IssueNumber = 1
	sessA.ID = "sess-a"

	sessB := makeSession(types.StateRunning, t2, "", false)
	sessB.IssueNumber = 2
	sessB.ID = "sess-b"

	st := &fakeStore{
		issues: map[int]types.Issue{
			1: {Number: 1, WorkspaceID: "ws"},
			2: {Number: 2, WorkspaceID: "ws"},
		},
		sessions: map[int][]types.Session{
			1: {sessA},
			2: {sessB},
		},
	}
	a := New(eventbus.New(), st)

	viewA, err := a.Assemble("ws", 1)
	if err != nil {
		t.Fatalf("Assemble issue 1: %v", err)
	}
	viewB, err := a.Assemble("ws", 2)
	if err != nil {
		t.Fatalf("Assemble issue 2: %v", err)
	}

	if viewA.ActiveSession == nil || viewA.ActiveSession.ID != "sess-a" {
		t.Errorf("issue 1 active = %v, want sess-a", viewA.ActiveSession)
	}
	if viewB.ActiveSession == nil || viewB.ActiveSession.ID != "sess-b" {
		t.Errorf("issue 2 active = %v, want sess-b", viewB.ActiveSession)
	}

	// Mutate via viewA; viewB must be unaffected.
	viewA.ActiveSession.BlockedReason = "should-not-leak"
	if viewB.ActiveSession.BlockedReason == "should-not-leak" {
		t.Error("mutation via viewA.ActiveSession leaked into viewB — pointer aliasing detected")
	}
}

// TestSelectSessions_BlockedInDoneIsSuppressed_ScenarioFromIssue113 reproduces
// the recurring "DONE cards glow blue/purple even though they're done" symptom
// users hit. The DB has stale state=blocked plan sessions in history; the
// assembler must NOT surface them as active_session or lastFail for a card
// that's reached the DONE column.
func TestSelectSessions_BlockedInDoneIsSuppressed_ScenarioFromIssue113(t *testing.T) {
	pn := 42
	sessions := []types.Session{
		makeSession(types.StateBlocked, t2, "BLOCKED: model thrashed", false),
		makeSession(types.StateBlocked, t1, "BLOCKED: token cap", false),
	}
	for i := range sessions {
		sessions[i].Mode = types.ModePlan
	}
	iss := types.Issue{Column: types.ColDone, PRNumber: &pn}
	active, paused, lastFail, _ := selectSessions(iss, sessions)
	if active != nil {
		t.Errorf("DONE card with state=blocked must NOT have active session, got %+v", active)
	}
	if paused != nil {
		t.Errorf("expected paused nil, got %+v", paused)
	}
	if lastFail != nil {
		t.Errorf("DONE column suppression must clear lastFail, got %+v", lastFail)
	}
}
