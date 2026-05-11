// Package cardstate provides a single authoritative CardState enum and the
// DeriveCardState function that maps IssueView inputs to that enum. All
// subsystems that previously derived card status independently should migrate
// to this package so the frontend becomes a dumb renderer of backend state
// (issue #30).
package cardstate

import (
	"fmt"

	"prismconductor/internal/types"
)

// CardState is the canonical visual state of a board card. It is derived
// entirely from backend data and emitted as part of IssueView so the frontend
// never needs to recompute it. The enum is intentionally exhaustive: every
// combination of (column, session, plan, PR, checks, conflicts) must map to
// exactly one value.
type CardState string

const (
	// CardStateTodo — no active work, no plan, default resting state.
	CardStateTodo CardState = "todo"
	// CardStatePlanReady — a plan exists and is waiting for user approval.
	CardStatePlanReady CardState = "plan_ready"
	// CardStatePlanning — an active plan-mode session is running.
	CardStatePlanning CardState = "planning"
	// CardStateNeedsAnswer — active session is waiting for user input.
	CardStateNeedsAnswer CardState = "needs_answer"
	// CardStateInProgress — an active execute-mode session is running.
	CardStateInProgress CardState = "in_progress"
	// CardStateBlocked — terminal blocked/failed session or plan failure.
	CardStateBlocked CardState = "blocked"
	// CardStateWaitingForPool — spawn queued because no pool slot is free.
	CardStateWaitingForPool CardState = "waiting_for_pool"
	// CardStateNeedsPR — execute completed but could not push; manual PR needed.
	CardStateNeedsPR CardState = "needs_pr"
	// CardStateOrphanQuestion — paused session whose question file is missing.
	CardStateOrphanQuestion CardState = "orphan_question"
	// CardStateMergeConflict — PR has merge conflicts with its base branch.
	CardStateMergeConflict CardState = "merge_conflict"
	// CardStateTestsFailing — PR's GitHub Actions checks are failing.
	CardStateTestsFailing CardState = "tests_failing"
	// CardStatePlanFailed — most recent plan session failed without a BLOCKED sentinel.
	CardStatePlanFailed CardState = "plan_failed"
	// CardStateReview — card is in the review column with a clean PR.
	CardStateReview CardState = "review"
	// CardStateDone — card is in the done column; work has shipped.
	CardStateDone CardState = "done"
	// CardStateRetryScheduled — execute failed with a retryable cause; the
	// scheduler has queued an automatic re-spawn with exponential backoff (#221).
	CardStateRetryScheduled CardState = "retry_scheduled"
	// CardStateWaitingForDep — card is in REVIEW with a green PR but at least
	// one cross-workspace dependency is still open (issue #210). Merge should
	// be held until the dep resolves.
	CardStateWaitingForDep CardState = "waiting_for_dep"
)

// PendingRetryInfo carries the retry schedule for a card in retry_scheduled
// state. Populated by the assembler from the in-memory scheduler (#221).
type PendingRetryInfo struct {
	Attempt     int                 `json:"attempt"`
	MaxAttempts int                 `json:"max_attempts"`
	NotBefore   int64               `json:"not_before"` // unix seconds
	LastCause   *types.FailureCause `json:"last_cause,omitempty"`
}

// CardStateDetails carries diagnostic context alongside CardState. Fields are
// populated only when relevant to the current state; absent fields are nil/zero.
type CardStateDetails struct {
	// Reason is a short human-readable explanation of the state.
	Reason string `json:"reason,omitempty"`
	// SessionID is the ID of the session responsible for this state.
	SessionID string `json:"session_id,omitempty"`
	// SessionMode is "plan" or "execute" for session-derived states.
	SessionMode string `json:"session_mode,omitempty"`
	// BlockedReason is the text captured after the BLOCKED: sentinel.
	BlockedReason string `json:"blocked_reason,omitempty"`
	// FailureCause is the structured cause for terminal failed/blocked sessions.
	FailureCause *types.FailureCause `json:"failure_cause,omitempty"`
	// RetryInfo is set when the card is in retry_scheduled state (#221).
	RetryInfo *PendingRetryInfo `json:"retry_info,omitempty"`
}

// PlanFailedInfo is the minimal representation of a silently-failed plan
// session (no BLOCKED sentinel captured). Mirrors issueview.PlanFailedInfo
// but lives here to break the cardstate→issueview import cycle.
type PlanFailedInfo struct {
	SessionID string `json:"session_id"`
	Reason    string `json:"reason,omitempty"`
}

// DeriveParams bundles all the data DeriveCardState needs from an IssueView.
// Callers should populate every field from the assembled view rather than
// constructing a partial struct.
type DeriveParams struct {
	Column         types.BoardColumn
	PRNumber       *int               // nil = no open PR
	WaitingForPool bool
	ActiveSession  *types.Session     // nil = no in-flight session
	PausedSession  *types.Session     // nil = no paused session
	LastFailure    *types.Session     // nil = no unacknowledged failure
	Plan           *types.Plan        // nil = no plan
	NeedsPRInfo    *types.NeedsPRInfo // nil = no needs-pr badge

	// Derived signals from the assembler's in-memory caches.
	HasTestsFailing   bool
	HasConflicts      bool
	HasOrphanQuestion bool
	PlanFailed        *PlanFailedInfo // nil = no silent plan failure

	// PendingRetry is set when the scheduler has queued an auto-retry for this
	// issue. Non-nil overrides the blocked state so the card shows a countdown
	// instead of a permanent error badge (#221).
	PendingRetry *PendingRetryInfo

	// WaitingForDep is set when the issue has an unresolved cross-workspace
	// dependency. Non-nil surfaces a waiting badge in REVIEW (issue #210).
	WaitingForDep *types.IssueDep
}

// DeriveCardState maps DeriveParams to a single CardState + detail blob.
// The precedence order mirrors Card.tsx's glow-state ladder and the plan §30
// specification so that backend and frontend converge to identical state:
//
//  1. Done column → done (always, regardless of session state)
//  2. Paused session + orphan question → orphan_question (stuck, recovery needed)
//  3. Paused session → plan_ready (awaiting question answer = treat as plan gate)
//  4. Active session blocked → blocked
//  5. Active session needs input → needs_answer
//  6. Active session plan-mode → planning
//  7. Active session execute-mode → in_progress
//  8. No active session + waiting_for_pool → waiting_for_pool
//  9. No active session + needs_pr → needs_pr
//  10. No active session + merge_conflict (PR open, any column) → merge_conflict
//  11. No active session + tests_failing (PR open, any column) → tests_failing
//  12. No active session + last_failure → blocked
//  13. No active session + plan_failed → plan_failed
//  14. Plan ready (not approved) → plan_ready
//  15. Review column with clean PR → review
//  16. Default → todo
func DeriveCardState(p DeriveParams) (CardState, CardStateDetails) {
	// DONE is authoritative regardless of any stale session rows.
	if p.Column == types.ColDone {
		return CardStateDone, CardStateDetails{}
	}

	// Paused session: user must answer a question before work can continue.
	if p.PausedSession != nil {
		if p.HasOrphanQuestion {
			return CardStateOrphanQuestion, CardStateDetails{
				SessionID:   p.PausedSession.ID,
				SessionMode: string(p.PausedSession.Mode),
				Reason:      "question file missing — session cannot resume",
			}
		}
		return CardStatePlanReady, CardStateDetails{
			SessionID:   p.PausedSession.ID,
			SessionMode: string(p.PausedSession.Mode),
			Reason:      "awaiting answer to mid-run question",
		}
	}

	// Active session arms.
	if p.ActiveSession != nil {
		s := p.ActiveSession
		switch {
		case s.State == types.StateBlocked:
			d := CardStateDetails{
				SessionID:     s.ID,
				SessionMode:   string(s.Mode),
				BlockedReason: s.BlockedReason,
				FailureCause:  s.FailureCause,
			}
			if s.BlockedReason != "" {
				d.Reason = s.BlockedReason
			} else {
				d.Reason = "BLOCKED: (no reason given)"
			}
			return CardStateBlocked, d
		case s.State == types.StateWaitingForInput:
			return CardStateNeedsAnswer, CardStateDetails{
				SessionID:   s.ID,
				SessionMode: string(s.Mode),
				Reason:      s.LastPrompt,
			}
		case s.Mode == types.ModePlan:
			return CardStatePlanning, CardStateDetails{
				SessionID:   s.ID,
				SessionMode: string(s.Mode),
			}
		default: // execute mode running
			return CardStateInProgress, CardStateDetails{
				SessionID:   s.ID,
				SessionMode: string(s.Mode),
			}
		}
	}

	// No active session — inspect terminal/ambient state.
	if p.WaitingForPool {
		return CardStateWaitingForPool, CardStateDetails{Reason: "waiting for an available agent pool"}
	}
	if p.NeedsPRInfo != nil && p.PRNumber == nil {
		return CardStateNeedsPR, CardStateDetails{
			Reason:      p.NeedsPRInfo.Reason,
			BlockedReason: p.NeedsPRInfo.Reason,
		}
	}
	if p.HasConflicts && p.PRNumber != nil {
		return CardStateMergeConflict, CardStateDetails{Reason: "PR has merge conflicts with its base branch"}
	}
	if p.HasTestsFailing && p.PRNumber != nil {
		return CardStateTestsFailing, CardStateDetails{Reason: "PR check runs are failing"}
	}
	if p.LastFailure != nil {
		// A pending auto-retry overrides the blocked state so the card shows a
		// countdown instead of a permanent error badge (#221).
		if p.PendingRetry != nil {
			return CardStateRetryScheduled, CardStateDetails{
				SessionID:    p.LastFailure.ID,
				SessionMode:  string(p.LastFailure.Mode),
				FailureCause: p.LastFailure.FailureCause,
				RetryInfo:    p.PendingRetry,
			}
		}
		d := CardStateDetails{
			SessionID:     p.LastFailure.ID,
			SessionMode:   string(p.LastFailure.Mode),
			BlockedReason: p.LastFailure.BlockedReason,
			FailureCause:  p.LastFailure.FailureCause,
		}
		if p.LastFailure.BlockedReason != "" {
			d.Reason = p.LastFailure.BlockedReason
		} else {
			d.Reason = "session ended without success"
		}
		return CardStateBlocked, d
	}
	if p.PlanFailed != nil {
		return CardStatePlanFailed, CardStateDetails{
			SessionID: p.PlanFailed.SessionID,
			Reason:    p.PlanFailed.Reason,
		}
	}
	if p.Plan != nil && p.Plan.ReadyToExecute && p.Plan.ApprovedAt == nil {
		if p.Column != types.ColReview && p.Column != types.ColDone {
			return CardStatePlanReady, CardStateDetails{Reason: "plan ready for approval"}
		}
	}
	if p.WaitingForDep != nil && p.Column == types.ColReview {
		return CardStateWaitingForDep, CardStateDetails{
			Reason: fmt.Sprintf("waiting for %s#%d to close before merging", p.WaitingForDep.WorkspaceID, p.WaitingForDep.Number),
		}
	}
	if p.Column == types.ColReview && p.PRNumber != nil {
		return CardStateReview, CardStateDetails{}
	}
	return CardStateTodo, CardStateDetails{}
}
