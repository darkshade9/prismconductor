// Package eventbus is the in-process pub/sub from PRISMCONDUCTOR_PLAN.md §7.
package eventbus

import (
	"sync"
	"time"
)

type EventType string

const (
	EvtGoalActivated     EventType = "goal_activated"
	EvtGoalUpdated       EventType = "goal_updated"
	EvtIssueAdded        EventType = "issue_added"
	EvtIssueClosed       EventType = "issue_closed"
	EvtIssueLabelChanged EventType = "issue_label_changed"
	EvtPlanReady         EventType = "plan_ready"
	EvtPROpened          EventType = "pr_opened"
	EvtPRMerged          EventType = "pr_merged"
	EvtPRClosedUnmerged  EventType = "pr_closed_unmerged"
	EvtPlanApproved      EventType = "plan_approved"
	EvtPlanRejected      EventType = "plan_rejected"
	EvtPlanRevised       EventType = "plan_revised"
	EvtWorkerSlotFreed   EventType = "worker_slot_freed"
	EvtWorkerBlocked     EventType = "worker_blocked"
	EvtCardMovedManually EventType = "card_moved_manually"
	EvtAgentCountChanged EventType = "agent_count_changed"
	EvtLabelsUpdated     EventType = "labels_updated"
	EvtAutoPullPausedChanged EventType = "auto_pull_paused_changed"
	EvtIssuesArchived        EventType = "issues_archived"
	// Issue #40: pending pool queue events.
	EvtPendingPoolEnqueued EventType = "pending_pool_enqueued"
	EvtPendingPoolDequeued EventType = "pending_pool_dequeued"

	// Issue #98: canonical IssueView assembler events.
	EvtIssueViewUpdated    EventType = "issue_view_updated"
	EvtSessionStateChanged EventType = "session_state_changed"

	// Issue #116: PR check-run failure detection and self-heal loop.
	EvtPRChecksFailed    EventType = "pr_checks_failed"
	EvtPRChecksRecovered EventType = "pr_checks_recovered"

	// Issue #124: PR merge-conflict detection and resolution worker.
	EvtPRConflictsDetected EventType = "pr_conflicts_detected"
	EvtPRConflictsResolved EventType = "pr_conflicts_resolved"

	// Issue #108: auto-refresh issues when a workspace is added.
	EvtWorkspaceAdded        EventType = "workspace_added"
	EvtWorkspaceFetchComplete EventType = "workspace_fetch_complete"

	// Issue #157: NEEDS_PR state — execute completed but push failed.
	EvtNeedsPR EventType = "needs_pr"

	// Issue #159: PR comment surface — inbound badge and outbound post.
	EvtPRCommentReceived EventType = "pr_comment_received"
	EvtPRCommentPosted   EventType = "pr_comment_posted"

	// Issue #194: orphan-branch detection and recovery.
	EvtOrphanPRDetected EventType = "orphan_pr_detected"
	EvtOrphanPRCleared  EventType = "orphan_pr_cleared"

	// Issue #209: workspace collections (Phase A of #208).
	EvtCollectionsUpdated EventType = "collections_updated"

	// Issue #221: auto-retry with exponential backoff for transient execute failures.
	EvtRetryScheduled EventType = "retry_scheduled"
	EvtRetryFired     EventType = "retry_fired"
	EvtRetryCancelled EventType = "retry_cancelled"
	EvtRetryExhausted EventType = "retry_exhausted"
)

type Event struct {
	Type      EventType
	Timestamp time.Time
	Payload   any
}

// WorkerSlotFreed is the payload shape for EvtWorkerSlotFreed (issue #27). The
// orchestrator uses PoolID to release the slot on the right pool registry
// entry; the prior bare-string-sessionID payload is replaced.
type WorkerSlotFreed struct {
	SessionID string `json:"session_id"`
	PoolID    string `json:"pool_id"`
}

// PendingPoolChange is the payload for EvtPendingPoolEnqueued / Dequeued (#40).
type PendingPoolChange struct {
	WorkspaceID string `json:"workspace_id"`
	IssueNumber int    `json:"issue_number"`
	Role        string `json:"role"`
}

// SessionStateChanged is the payload for EvtSessionStateChanged (#98). Published
// from handleSessionStateChange so the IssueView assembler can react to every
// session transition without subscribing to Wails-only events.
type SessionStateChanged struct {
	WorkspaceID string `json:"workspace_id"`
	IssueNumber int    `json:"issue_number"`
	SessionID   string `json:"session_id"`
}

// PRChecksFailed is the payload for EvtPRChecksFailed (#116). Published when the
// poller detects a failing-edge transition (passing → any_failed) on a REVIEW-column
// PR's check runs. Only includes checks owned by this repo's GitHub Actions workflows.
type PRChecksFailed struct {
	WorkspaceID         string   `json:"workspace_id"`
	IssueNumber         int      `json:"issue_number"`
	PRNumber            int      `json:"pr_number"`
	HeadSHA             string   `json:"head_sha"`
	FailingJobs         []string `json:"failing_jobs"`
	FailingCheckRunURLs []string `json:"failing_check_run_urls"`
	RunIDs              []int64  `json:"run_ids"`
}

// PRChecksRecovered is the payload for EvtPRChecksRecovered (#116). Published when
// all check runs on a PR HEAD SHA pass (after a prior failure).
type PRChecksRecovered struct {
	WorkspaceID string `json:"workspace_id"`
	IssueNumber int    `json:"issue_number"`
	PRNumber    int    `json:"pr_number"`
	HeadSHA     string `json:"head_sha"`
}

// PRConflictsDetected is the payload for EvtPRConflictsDetected (#124). Published when
// the poller detects that a REVIEW-column PR's mergeable_state transitions to "dirty"
// (i.e. conflicts with the base branch). ConflictingFiles lists the PR's changed files;
// the actual conflicting subset is resolved by the worker at rebase time.
type PRConflictsDetected struct {
	WorkspaceID      string   `json:"workspace_id"`
	IssueNumber      int      `json:"issue_number"`
	PRNumber         int      `json:"pr_number"`
	Base             string   `json:"base"`
	Head             string   `json:"head"`
	ConflictingFiles []string `json:"conflicting_files"`
}

// PRConflictsResolved is the payload for EvtPRConflictsResolved (#124). Published when
// a PR's mergeable_state transitions away from "dirty" (conflict cleared or PR closed).
type PRConflictsResolved struct {
	WorkspaceID string `json:"workspace_id"`
	IssueNumber int    `json:"issue_number"`
	PRNumber    int    `json:"pr_number"`
}

// WorkspaceFetchComplete is the payload for EvtWorkspaceFetchComplete (#108). Published
// after an immediate fetch triggered by AddWorkspace completes (success or failure).
type WorkspaceFetchComplete struct {
	WorkspaceID   string `json:"workspace_id"`
	WorkspaceName string `json:"workspace_name"`
	Success       bool   `json:"success"`
	IssueCount    int    `json:"issue_count"`
	Error         string `json:"error,omitempty"`
}

// NeedsPREvent is the payload for EvtNeedsPR (#157). Published when an execute
// session emits the NEEDS_PR: sentinel, signalling that the code is committed
// locally but the push could not complete.
type NeedsPREvent struct {
	WorkspaceID string `json:"workspace_id"`
	IssueNumber int    `json:"issue_number"`
	Branch      string `json:"branch"`
	WorktreeDir string `json:"worktree_dir"`
	Reason      string `json:"reason"`
	Kind        string `json:"kind"` // "commit_signing" | "push"
}

// PRCommentReceived is the payload for EvtPRCommentReceived (#159). Published
// when the poller detects a new inbound PR comment not authored by the
// conductor's own gh user.
type PRCommentReceived struct {
	WorkspaceID string `json:"workspace_id"`
	IssueNumber int    `json:"issue_number"`
	PRNumber    int    `json:"pr_number"`
	CommentID   int64  `json:"comment_id"`
	Author      string `json:"author"`
}

// OrphanPRDetected is the payload for EvtOrphanPRDetected (#194).
type OrphanPRDetected struct {
	WorkspaceID string `json:"workspace_id"`
	IssueNumber int    `json:"issue_number"`
	Branch      string `json:"branch"`
}

// OrphanPRCleared is the payload for EvtOrphanPRCleared (#194).
type OrphanPRCleared struct {
	WorkspaceID string `json:"workspace_id"`
	IssueNumber int    `json:"issue_number"`
}

// RetryScheduled is the payload for EvtRetryScheduled (#221). Published when
// the scheduler queues an automatic re-spawn for a transient execute failure.
type RetryScheduled struct {
	WorkspaceID string `json:"workspace_id"`
	IssueNumber int    `json:"issue_number"`
	Attempt     int    `json:"attempt"`
	MaxAttempts int    `json:"max_attempts"`
	NotBefore   int64  `json:"not_before"` // unix seconds
}

// RetryFired is the payload for EvtRetryFired (#221). Published immediately
// before the scheduler calls the fire callback.
type RetryFired struct {
	WorkspaceID string `json:"workspace_id"`
	IssueNumber int    `json:"issue_number"`
	Attempt     int    `json:"attempt"`
}

// RetryCancelled is the payload for EvtRetryCancelled (#221). Published when
// a pending retry is cancelled (user action or budget exceeded).
type RetryCancelled struct {
	WorkspaceID string `json:"workspace_id"`
	IssueNumber int    `json:"issue_number"`
	Reason      string `json:"reason,omitempty"` // "user" | "budget" | "policy"
}

// RetryExhausted is the payload for EvtRetryExhausted (#221). Published when
// all attempts have been consumed and the card should show a permanent error.
type RetryExhausted struct {
	WorkspaceID string `json:"workspace_id"`
	IssueNumber int    `json:"issue_number"`
	Attempts    int    `json:"attempts"`
}

type Handler func(Event)

type Bus struct {
	mu       sync.RWMutex
	handlers []Handler
}

func New() *Bus { return &Bus{} }

func (b *Bus) Subscribe(h Handler) {
	b.mu.Lock()
	b.handlers = append(b.handlers, h)
	b.mu.Unlock()
}

func (b *Bus) Publish(t EventType, payload any) {
	evt := Event{Type: t, Timestamp: time.Now(), Payload: payload}
	b.mu.RLock()
	hs := append([]Handler(nil), b.handlers...)
	b.mu.RUnlock()
	for _, h := range hs {
		go h(evt)
	}
}
