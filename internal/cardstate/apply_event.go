package cardstate

import (
	"time"

	"prismconductor/internal/types"
)

// EventKind classifies what triggered a card state transition.
type EventKind string

const (
	EvtSessionStarted      EventKind = "session_started"
	EvtSessionCompleted    EventKind = "session_completed"
	EvtSessionBlocked      EventKind = "session_blocked"
	EvtSessionFailed       EventKind = "session_failed"
	EvtSessionPaused       EventKind = "session_paused"       // paused_for_question
	EvtSessionNeedsPR      EventKind = "session_needs_pr"
	EvtPROpened            EventKind = "pr_opened"
	EvtPRMerged            EventKind = "pr_merged"
	EvtPRClosedUnmerged    EventKind = "pr_closed_unmerged"
	EvtPRConflictsDetected EventKind = "pr_conflicts_detected"
	EvtPRConflictsResolved EventKind = "pr_conflicts_resolved"
	EvtPRChecksFailed      EventKind = "pr_checks_failed"
	EvtPRChecksRecovered   EventKind = "pr_checks_recovered"
	EvtIssueClosed         EventKind = "issue_closed"
	EvtPoolEnqueued        EventKind = "pool_enqueued"
	EvtPoolDequeued        EventKind = "pool_dequeued"
	// EvtDepWaiting fires when an unresolved cross-workspace dep is detected
	// (issue #210). DepInfo carries the blocking dep identity.
	EvtDepWaiting  EventKind = "dep_waiting"
	// EvtDepResolved fires when all cross-workspace deps for an issue are
	// satisfied and WaitingForDep is cleared (issue #210).
	EvtDepResolved EventKind = "dep_resolved"
)

// Event is the input to ApplyEvent. It carries the kind plus any additional
// context needed to determine the new card state and side-effects.
type Event struct {
	Kind         EventKind
	Session      *types.Session    // populated for session-origin events
	FailureCause *types.FailureCause // populated for terminal failure events
	PRNumber     *int
	PRURL        string
	OccurredAt   time.Time
	// DepInfo is populated for EvtDepWaiting events (issue #210).
	DepInfo *types.IssueDep
}

// SideEffect is an explicit action that must be performed after a state
// transition is persisted. Running side-effects through this enum keeps
// them auditable and centralised instead of scattered across call sites.
type SideEffect string

const (
	// SideEffectClearConflictCache removes the in-memory conflict entry for the
	// issue from the assembler's cache.
	SideEffectClearConflictCache SideEffect = "clear_conflict_cache"
	// SideEffectPokePoller asks the GitHub poller to re-check the issue sooner
	// than its normal 5-minute cadence.
	SideEffectPokePoller SideEffect = "poke_poller"
	// SideEffectEmitToast fires a user-visible in-app toast notification.
	SideEffectEmitToast SideEffect = "emit_toast"
	// SideEffectReassembleView forces the issueview Assembler to rebuild and
	// re-emit the IssueView for this card.
	SideEffectReassembleView SideEffect = "reassemble_view"
)

// TransitionRecord is written to the card_state_transitions table by callers
// that have a store reference. ApplyEvent returns it so the caller can persist
// it in the same transaction as the issue/session update.
type TransitionRecord struct {
	WorkspaceID string
	IssueNumber int
	FromState   CardState
	ToState     CardState
	EventKind   EventKind
	OccurredAt  time.Time
}

// ApplyEvent derives the new CardState from the current IssueView inputs plus
// the incoming event, and returns the side-effects that must be run after the
// transition is persisted.
//
// Phase 1: this function is additive — it derives new state but does not yet
// mutate the store directly. Callers are responsible for persisting the
// TransitionRecord and running the returned SideEffects. Full wiring of all
// call sites (session/manager, github/poll, store/issues) is Phase 3 (#30).
func ApplyEvent(
	workspaceID string,
	issueNumber int,
	current DeriveParams,
	ev Event,
) (CardState, CardStateDetails, TransitionRecord, []SideEffect) {
	fromState, _ := DeriveCardState(current)

	// Mutate a copy of the params to reflect what the event changes.
	next := current
	switch ev.Kind {
	case EvtSessionStarted:
		if ev.Session != nil {
			next.ActiveSession = ev.Session
			next.PausedSession = nil
		}
	case EvtSessionCompleted:
		next.ActiveSession = nil
	case EvtSessionBlocked, EvtSessionFailed:
		if ev.Session != nil {
			next.LastFailure = ev.Session
		}
		next.ActiveSession = nil
	case EvtSessionPaused:
		if ev.Session != nil {
			next.PausedSession = ev.Session
		}
		next.ActiveSession = nil
	case EvtSessionNeedsPR:
		next.ActiveSession = nil
		if ev.Session != nil {
			next.NeedsPRInfo = &types.NeedsPRInfo{Reason: ev.Session.BlockedReason}
		}
	case EvtPROpened:
		next.PRNumber = ev.PRNumber
		next.NeedsPRInfo = nil
	case EvtPRMerged:
		next.Column = types.ColDone
		next.PRNumber = ev.PRNumber
		next.HasConflicts = false
		next.HasTestsFailing = false
	case EvtPRClosedUnmerged:
		next.PRNumber = nil
		next.HasConflicts = false
		next.HasTestsFailing = false
	case EvtPRConflictsDetected:
		next.HasConflicts = true
	case EvtPRConflictsResolved:
		next.HasConflicts = false
	case EvtPRChecksFailed:
		next.HasTestsFailing = true
	case EvtPRChecksRecovered:
		next.HasTestsFailing = false
	case EvtIssueClosed:
		next.Column = types.ColDone
	case EvtPoolEnqueued:
		next.WaitingForPool = true
	case EvtPoolDequeued:
		next.WaitingForPool = false
	case EvtDepWaiting:
		next.WaitingForDep = ev.DepInfo
	case EvtDepResolved:
		next.WaitingForDep = nil
	}

	toState, details := DeriveCardState(next)

	record := TransitionRecord{
		WorkspaceID: workspaceID,
		IssueNumber: issueNumber,
		FromState:   fromState,
		ToState:     toState,
		EventKind:   ev.Kind,
		OccurredAt:  ev.OccurredAt,
	}
	if record.OccurredAt.IsZero() {
		record.OccurredAt = time.Now().UTC()
	}

	var effects []SideEffect
	effects = append(effects, SideEffectReassembleView)
	switch ev.Kind {
	case EvtPRConflictsResolved, EvtPRMerged, EvtPRClosedUnmerged:
		effects = append(effects, SideEffectClearConflictCache)
	case EvtSessionCompleted, EvtSessionBlocked, EvtSessionFailed:
		effects = append(effects, SideEffectPokePoller)
	}
	if fromState != toState {
		effects = append(effects, SideEffectEmitToast)
	}

	return toState, details, record, effects
}
