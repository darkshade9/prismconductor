package issueview

import (
	"testing"
	"time"

	"prismconductor/internal/eventbus"
	"prismconductor/internal/types"
)

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
	active, paused, lastFail := selectSessions(iss, sessions)
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
	_, paused, _ := selectSessions(iss, []types.Session{s})
	if paused == nil || paused.State != types.StatePausedForQuestion {
		t.Fatal("expected paused session")
	}
}

func TestSelectSessions_LastFailureTracked(t *testing.T) {
	sessions := []types.Session{makeSession(types.StateFailed, t1, "BLOCKED: reason", false)}
	_, _, lastFail := selectSessions(types.Issue{Column: types.ColTodo}, sessions)
	if lastFail == nil || lastFail.BlockedReason != "BLOCKED: reason" {
		t.Fatalf("expected lastFail with reason, got %v", lastFail)
	}
}

func TestSelectSessions_LastFailureSuppressedInReview(t *testing.T) {
	sessions := []types.Session{makeSession(types.StateFailed, t1, "BLOCKED: x", false)}
	_, _, lastFail := selectSessions(types.Issue{Column: types.ColReview}, sessions)
	if lastFail != nil {
		t.Error("lastFail should be suppressed in review")
	}
}

func TestSelectSessions_LastFailureSuppressedInDone(t *testing.T) {
	sessions := []types.Session{makeSession(types.StateFailed, t1, "BLOCKED: x", false)}
	_, _, lastFail := selectSessions(types.Issue{Column: types.ColDone}, sessions)
	if lastFail != nil {
		t.Error("lastFail should be suppressed in done")
	}
}

func TestSelectSessions_LastFailureSuppressedByLaterSuccess(t *testing.T) {
	sessions := []types.Session{
		makeSession(types.StateCompleted, t3, "", false),
		makeSession(types.StateFailed, t1, "BLOCKED: old", false),
	}
	_, _, lastFail := selectSessions(types.Issue{Column: types.ColPlan}, sessions)
	if lastFail != nil {
		t.Error("lastFail should be suppressed by later completed session")
	}
}

func TestSelectSessions_AcknowledgedFailureIgnored(t *testing.T) {
	sessions := []types.Session{makeSession(types.StateFailed, t1, "BLOCKED: acked", true)}
	_, _, lastFail := selectSessions(types.Issue{Column: types.ColTodo}, sessions)
	if lastFail != nil {
		t.Error("acknowledged failure should not surface as lastFail")
	}
}

func TestSelectSessions_MostRecentFailureWins(t *testing.T) {
	sessions := []types.Session{
		makeSession(types.StateFailed, t2, "newer", false),
		makeSession(types.StateFailed, t1, "older", false),
	}
	_, _, lastFail := selectSessions(types.Issue{Column: types.ColTodo}, sessions)
	if lastFail == nil || lastFail.BlockedReason != "newer" {
		t.Errorf("expected newer reason, got %v", lastFail)
	}
}

func TestSelectSessions_NoReason_NotTrackedAsFailure(t *testing.T) {
	sessions := []types.Session{makeSession(types.StateFailed, t1, "", false)}
	_, _, lastFail := selectSessions(types.Issue{Column: types.ColTodo}, sessions)
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
