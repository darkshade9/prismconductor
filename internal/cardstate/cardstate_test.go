package cardstate_test

import (
	"testing"
	"time"

	"prismconductor/internal/cardstate"
	"prismconductor/internal/types"
)

func ptr[T any](v T) *T { return &v }

func activeSession(mode types.SessionMode, state types.SessionState) *types.Session {
	return &types.Session{
		ID:        "sess-1",
		Mode:      mode,
		State:     state,
		StartedAt: time.Now(),
	}
}

func blockedSession(mode types.SessionMode, reason string) *types.Session {
	return &types.Session{
		ID:            "sess-blocked",
		Mode:          mode,
		State:         types.StateBlocked,
		BlockedReason: reason,
		StartedAt:     time.Now(),
	}
}

func failedSession(mode types.SessionMode, reason string) *types.Session {
	return &types.Session{
		ID:            "sess-fail",
		Mode:          mode,
		State:         types.StateFailed,
		BlockedReason: reason,
		StartedAt:     time.Now(),
	}
}

func pausedSession(qid string) *types.Session {
	return &types.Session{
		ID:                "sess-paused",
		Mode:              types.ModeExecute,
		State:             types.StatePausedForQuestion,
		PendingQuestionID: qid,
		StartedAt:         time.Now(),
	}
}

func approvedPlan() *types.Plan {
	now := time.Now()
	return &types.Plan{
		ReadyToExecute: true,
		ApprovedAt:     &now,
	}
}

func readyPlan() *types.Plan {
	return &types.Plan{ReadyToExecute: true}
}

func TestDeriveCardState(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name        string
		params      cardstate.DeriveParams
		wantState   cardstate.CardState
		wantReason  string // substring match; empty = don't check
		wantSession string // expected details.SessionID; empty = don't check
	}{
		// ── Done column ──────────────────────────────────────────────────────
		{
			name:      "done column always done",
			params:    cardstate.DeriveParams{Column: types.ColDone},
			wantState: cardstate.CardStateDone,
		},
		{
			name: "done column with active session still done",
			params: cardstate.DeriveParams{
				Column:        types.ColDone,
				ActiveSession: activeSession(types.ModeExecute, types.StateRunning),
			},
			wantState: cardstate.CardStateDone,
		},

		// ── Paused session ───────────────────────────────────────────────────
		{
			name: "paused session no orphan → plan_ready",
			params: cardstate.DeriveParams{
				Column:        types.ColInProgress,
				PausedSession: pausedSession("q-123"),
			},
			wantState:   cardstate.CardStatePlanReady,
			wantSession: "sess-paused",
		},
		{
			name: "paused session with orphan question",
			params: cardstate.DeriveParams{
				Column:            types.ColInProgress,
				PausedSession:     pausedSession("q-missing"),
				HasOrphanQuestion: true,
			},
			wantState:   cardstate.CardStateOrphanQuestion,
			wantSession: "sess-paused",
		},

		// ── Active session ───────────────────────────────────────────────────
		{
			name: "active execute session running → in_progress",
			params: cardstate.DeriveParams{
				Column:        types.ColInProgress,
				ActiveSession: activeSession(types.ModeExecute, types.StateRunning),
			},
			wantState:   cardstate.CardStateInProgress,
			wantSession: "sess-1",
		},
		{
			name: "active plan session running → planning",
			params: cardstate.DeriveParams{
				Column:        types.ColPlan,
				ActiveSession: activeSession(types.ModePlan, types.StateRunning),
			},
			wantState:   cardstate.CardStatePlanning,
			wantSession: "sess-1",
		},
		{
			name: "active session state=blocked → blocked",
			params: cardstate.DeriveParams{
				Column:        types.ColInProgress,
				ActiveSession: blockedSession(types.ModeExecute, "lint failed"),
			},
			wantState:   cardstate.CardStateBlocked,
			wantSession: "sess-blocked",
			wantReason:  "lint failed",
		},
		{
			name: "active session state=blocked no reason",
			params: cardstate.DeriveParams{
				Column:        types.ColInProgress,
				ActiveSession: blockedSession(types.ModeExecute, ""),
			},
			wantState: cardstate.CardStateBlocked,
		},
		{
			name: "active session waiting_for_input plan → needs_answer",
			params: cardstate.DeriveParams{
				Column:        types.ColPlan,
				ActiveSession: activeSession(types.ModePlan, types.StateWaitingForInput),
			},
			wantState: cardstate.CardStateNeedsAnswer,
		},
		{
			name: "active session waiting_for_input execute → needs_answer",
			params: cardstate.DeriveParams{
				Column:        types.ColInProgress,
				ActiveSession: activeSession(types.ModeExecute, types.StateWaitingForInput),
			},
			wantState: cardstate.CardStateNeedsAnswer,
		},

		// ── No active session ─────────────────────────────────────────────────
		{
			name: "waiting_for_pool, no active session",
			params: cardstate.DeriveParams{
				Column:         types.ColTodo,
				WaitingForPool: true,
			},
			wantState: cardstate.CardStateWaitingForPool,
		},
		{
			name: "needs_pr_info set, no PR, no active session",
			params: cardstate.DeriveParams{
				Column:      types.ColInProgress,
				NeedsPRInfo: &types.NeedsPRInfo{Reason: "signing key unavailable"},
			},
			wantState:  cardstate.CardStateNeedsPR,
			wantReason: "signing key unavailable",
		},
		{
			name: "needs_pr_info but PR already attached → not needs_pr",
			params: cardstate.DeriveParams{
				Column:      types.ColReview,
				PRNumber:    ptr(42),
				NeedsPRInfo: &types.NeedsPRInfo{Reason: "old reason"},
			},
			wantState: cardstate.CardStateReview,
		},
		{
			name: "merge conflict in review column",
			params: cardstate.DeriveParams{
				Column:      types.ColReview,
				PRNumber:    ptr(7),
				HasConflicts: true,
			},
			wantState: cardstate.CardStateMergeConflict,
		},
		{
			name: "merge conflict NOT in review column → ignored",
			params: cardstate.DeriveParams{
				Column:       types.ColInProgress,
				HasConflicts: true,
			},
			wantState: cardstate.CardStateTodo,
		},
		{
			name: "tests failing in review column",
			params: cardstate.DeriveParams{
				Column:          types.ColReview,
				PRNumber:        ptr(7),
				HasTestsFailing: true,
			},
			wantState: cardstate.CardStateTestsFailing,
		},
		{
			name: "tests failing NOT in review column → ignored",
			params: cardstate.DeriveParams{
				Column:          types.ColInProgress,
				HasTestsFailing: true,
			},
			wantState: cardstate.CardStateTodo,
		},
		{
			name: "last_failure (blocked reason) → blocked",
			params: cardstate.DeriveParams{
				Column:      types.ColTodo,
				LastFailure: blockedSession(types.ModeExecute, "tests failed"),
			},
			wantState:  cardstate.CardStateBlocked,
			wantReason: "tests failed",
		},
		{
			name: "last_failure (failed, no reason) → blocked with default reason",
			params: cardstate.DeriveParams{
				Column:      types.ColPlan,
				LastFailure: failedSession(types.ModePlan, ""),
			},
			wantState:  cardstate.CardStateBlocked,
			wantReason: "session ended without success",
		},
		{
			name: "plan_failed with no last_failure",
			params: cardstate.DeriveParams{
				Column: types.ColPlan,
				PlanFailed: &cardstate.PlanFailedInfo{
					SessionID: "plan-sess-99",
					Reason:    "plan session ended without success",
				},
			},
			wantState:   cardstate.CardStatePlanFailed,
			wantSession: "plan-sess-99",
		},
		{
			name: "last_failure takes precedence over plan_failed",
			params: cardstate.DeriveParams{
				Column:      types.ColPlan,
				LastFailure: blockedSession(types.ModePlan, "reason A"),
				PlanFailed: &cardstate.PlanFailedInfo{
					SessionID: "plan-sess-99",
				},
			},
			wantState: cardstate.CardStateBlocked,
		},

		// ── Plan ready ───────────────────────────────────────────────────────
		{
			name: "plan ready (not approved) in todo column → plan_ready",
			params: cardstate.DeriveParams{
				Column: types.ColTodo,
				Plan:   readyPlan(),
			},
			wantState: cardstate.CardStatePlanReady,
		},
		{
			name: "plan ready (not approved) in plan column → plan_ready",
			params: cardstate.DeriveParams{
				Column: types.ColPlan,
				Plan:   readyPlan(),
			},
			wantState: cardstate.CardStatePlanReady,
		},
		{
			name: "plan ready but already approved → not plan_ready",
			params: cardstate.DeriveParams{
				Column: types.ColTodo,
				Plan:   approvedPlan(),
			},
			wantState: cardstate.CardStateTodo,
		},
		{
			name: "plan ready but column=review → suppressed",
			params: cardstate.DeriveParams{
				Column:   types.ColReview,
				Plan:     readyPlan(),
				PRNumber: ptr(5),
			},
			wantState: cardstate.CardStateReview,
		},
		{
			name: "plan ready but column=done → suppressed",
			params: cardstate.DeriveParams{
				Column: types.ColDone,
				Plan:   readyPlan(),
			},
			wantState: cardstate.CardStateDone,
		},

		// ── Review ───────────────────────────────────────────────────────────
		{
			name: "review column with PR, clean",
			params: cardstate.DeriveParams{
				Column:   types.ColReview,
				PRNumber: ptr(42),
			},
			wantState: cardstate.CardStateReview,
		},
		{
			name: "review column without PR",
			params: cardstate.DeriveParams{
				Column: types.ColReview,
			},
			wantState: cardstate.CardStateTodo,
		},

		// ── Default todo ─────────────────────────────────────────────────────
		{
			name:      "empty params → todo",
			params:    cardstate.DeriveParams{Column: types.ColTodo},
			wantState: cardstate.CardStateTodo,
		},
		{
			name:      "in_progress column no session → todo",
			params:    cardstate.DeriveParams{Column: types.ColInProgress},
			wantState: cardstate.CardStateTodo,
		},

		// ── Priority ordering ─────────────────────────────────────────────────
		{
			name: "active session beats waiting_for_pool",
			params: cardstate.DeriveParams{
				Column:         types.ColInProgress,
				WaitingForPool: true,
				ActiveSession:  activeSession(types.ModeExecute, types.StateRunning),
			},
			wantState: cardstate.CardStateInProgress,
		},
		{
			name: "paused session beats last_failure",
			params: cardstate.DeriveParams{
				Column:        types.ColInProgress,
				PausedSession: pausedSession("q-1"),
				LastFailure:   blockedSession(types.ModeExecute, "old failure"),
			},
			wantState: cardstate.CardStatePlanReady,
		},
		{
			name: "active session beats last_failure",
			params: cardstate.DeriveParams{
				Column:        types.ColInProgress,
				ActiveSession: activeSession(types.ModeExecute, types.StateRunning),
				LastFailure:   blockedSession(types.ModeExecute, "old failure"),
			},
			wantState: cardstate.CardStateInProgress,
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, details := cardstate.DeriveCardState(tt.params)
			if got != tt.wantState {
				t.Errorf("DeriveCardState() = %q, want %q", got, tt.wantState)
			}
			if tt.wantReason != "" {
				if details.Reason == "" {
					t.Errorf("details.Reason empty, want substring %q", tt.wantReason)
				} else {
					found := false
					for i := range details.Reason {
						if i+len(tt.wantReason) <= len(details.Reason) &&
							details.Reason[i:i+len(tt.wantReason)] == tt.wantReason {
							found = true
							break
						}
					}
					if !found {
						t.Errorf("details.Reason = %q, want substring %q", details.Reason, tt.wantReason)
					}
				}
			}
			if tt.wantSession != "" && details.SessionID != tt.wantSession {
				t.Errorf("details.SessionID = %q, want %q", details.SessionID, tt.wantSession)
			}
		})
	}
}

func TestDeriveCardState_FailureCausePassthrough(t *testing.T) {
	t.Parallel()
	cause := &types.FailureCause{Kind: "exit_code", ExitCode: 1, Reason: "non-zero exit"}
	sess := &types.Session{
		ID:            "sess-fc",
		Mode:          types.ModeExecute,
		State:         types.StateFailed,
		BlockedReason: "build failed",
		FailureCause:  cause,
		StartedAt:     time.Now(),
	}
	params := cardstate.DeriveParams{
		Column:      types.ColTodo,
		LastFailure: sess,
	}
	_, details := cardstate.DeriveCardState(params)
	if details.FailureCause == nil {
		t.Fatal("FailureCause should be propagated into details")
	}
	if details.FailureCause.Kind != "exit_code" {
		t.Errorf("FailureCause.Kind = %q, want exit_code", details.FailureCause.Kind)
	}
}

func TestApplyEvent_BasicTransitions(t *testing.T) {
	t.Parallel()
	base := cardstate.DeriveParams{Column: types.ColTodo}

	t.Run("pool enqueued", func(t *testing.T) {
		t.Parallel()
		got, _, _, _ := cardstate.ApplyEvent("ws1", 1, base, cardstate.Event{Kind: cardstate.EvtPoolEnqueued})
		if got != cardstate.CardStateWaitingForPool {
			t.Errorf("got %q, want waiting_for_pool", got)
		}
	})
	t.Run("pool dequeued", func(t *testing.T) {
		t.Parallel()
		pooled := base
		pooled.WaitingForPool = true
		got, _, _, _ := cardstate.ApplyEvent("ws1", 1, pooled, cardstate.Event{Kind: cardstate.EvtPoolDequeued})
		if got != cardstate.CardStateTodo {
			t.Errorf("got %q, want todo", got)
		}
	})
	t.Run("pr merged → done", func(t *testing.T) {
		t.Parallel()
		review := cardstate.DeriveParams{Column: types.ColReview, PRNumber: ptr(5)}
		got, _, _, _ := cardstate.ApplyEvent("ws1", 5, review, cardstate.Event{Kind: cardstate.EvtPRMerged, PRNumber: ptr(5)})
		if got != cardstate.CardStateDone {
			t.Errorf("got %q, want done", got)
		}
	})
	t.Run("checks failed in review → tests_failing", func(t *testing.T) {
		t.Parallel()
		review := cardstate.DeriveParams{Column: types.ColReview, PRNumber: ptr(3)}
		got, _, _, _ := cardstate.ApplyEvent("ws1", 3, review, cardstate.Event{Kind: cardstate.EvtPRChecksFailed})
		if got != cardstate.CardStateTestsFailing {
			t.Errorf("got %q, want tests_failing", got)
		}
	})
}

func TestApplyEvent_SideEffects(t *testing.T) {
	t.Parallel()
	base := cardstate.DeriveParams{Column: types.ColTodo}
	_, _, _, effects := cardstate.ApplyEvent("ws1", 1, base, cardstate.Event{Kind: cardstate.EvtPoolEnqueued})

	hasEffect := func(want cardstate.SideEffect) bool {
		for _, e := range effects {
			if e == want {
				return true
			}
		}
		return false
	}
	if !hasEffect(cardstate.SideEffectReassembleView) {
		t.Error("expected SideEffectReassembleView always present")
	}
}

func TestApplyEvent_TransitionRecord(t *testing.T) {
	t.Parallel()
	base := cardstate.DeriveParams{Column: types.ColTodo}
	_, _, rec, _ := cardstate.ApplyEvent("ws42", 99, base, cardstate.Event{Kind: cardstate.EvtPoolEnqueued})
	if rec.WorkspaceID != "ws42" {
		t.Errorf("record.WorkspaceID = %q, want ws42", rec.WorkspaceID)
	}
	if rec.IssueNumber != 99 {
		t.Errorf("record.IssueNumber = %d, want 99", rec.IssueNumber)
	}
	if rec.FromState != cardstate.CardStateTodo {
		t.Errorf("record.FromState = %q, want todo", rec.FromState)
	}
	if rec.ToState != cardstate.CardStateWaitingForPool {
		t.Errorf("record.ToState = %q, want waiting_for_pool", rec.ToState)
	}
	if rec.EventKind != cardstate.EvtPoolEnqueued {
		t.Errorf("record.EventKind = %q, want pool_enqueued", rec.EventKind)
	}
	if rec.OccurredAt.IsZero() {
		t.Error("record.OccurredAt should not be zero")
	}
}
