// Package issueview assembles a canonical per-card view from multiple backend
// sources and emits bus.issue_view_updated whenever the derived state changes.
// This eliminates the frontend multi-store reconciliation that caused recurring
// state-drift bugs (issue #98).
package issueview

import "prismconductor/internal/types"

// PoolBadge is the resolved provider badge for a card. Derived from the
// most-recent session's pool_id so the card can show the provider icon without
// accessing the pools store directly.
type PoolBadge struct {
	PoolID   string `json:"pool_id"`
	Name     string `json:"name"`
	Provider string `json:"provider"`
}

// TestsFailingInfo is populated when a REVIEW-column PR's GitHub Actions checks
// are failing. Cleared automatically when checks recover or the PR merges (#116).
type TestsFailingInfo struct {
	FailingJobs         []string `json:"failing_jobs"`
	FailingCheckRunURLs []string `json:"failing_check_run_urls"`
	HeadSHA             string   `json:"head_sha"`
	SelfHealAttempts    int      `json:"self_heal_attempts,omitempty"`
	AttemptCap          int      `json:"attempt_cap,omitempty"`
	MaxAttemptsReached  bool     `json:"max_attempts_reached,omitempty"`
}

// ConflictsInfo is populated when a REVIEW-column PR has merge conflicts with
// its base branch (mergeable_state == "dirty"). Cleared when conflicts resolve
// or the PR merges/closes (#124).
type ConflictsInfo struct {
	PRNumber         int      `json:"pr_number"`
	Base             string   `json:"base"`
	Head             string   `json:"head"`
	ConflictingFiles []string `json:"conflicting_files"`
}

// OrphanQuestionInfo is populated when a paused_for_question session's question
// file is absent from disk (#153). The frontend uses this to render a recovery
// badge and enable the Recover action without re-checking the filesystem.
type OrphanQuestionInfo struct {
	PendingQuestionID string `json:"pending_question_id"`
	// Since is the Unix epoch of the session's started_at, used to compute
	// how long the card has been stuck.
	Since int64 `json:"since"`
}

// IssueView is the single canonical read model for a board card. Assembled on
// the backend and emitted as bus.issue_view_updated on every change so the
// frontend can render without reconciling multiple stores.
type IssueView struct {
	Issue         types.Issue   `json:"issue"`
	LatestPlan    *types.Plan   `json:"latest_plan,omitempty"`
	ActiveSession *types.Session `json:"active_session,omitempty"`
	PausedSession *types.Session `json:"paused_session,omitempty"`
	LastFailure   *types.Session `json:"last_failure,omitempty"`
	// LastSession is the most recent terminal session regardless of outcome.
	// Used by the CostChip hover tooltip to show per-session token breakdown
	// (issue #101). Nil when no terminal session exists.
	LastSession      *types.Session      `json:"last_session,omitempty"`
	PoolBadge        *PoolBadge          `json:"pool_badge,omitempty"`
	DerivedColumn    types.BoardColumn   `json:"derived_column"`
	TestsFailingInfo *TestsFailingInfo   `json:"tests_failing_info,omitempty"`
	ConflictsInfo    *ConflictsInfo      `json:"conflicts_info,omitempty"`
	// OrphanQuestion is set when PausedSession has a pending_question_id but
	// the corresponding question file is missing from disk (#153). The
	// frontend renders a BLOCKED badge and a Recover action.
	OrphanQuestion *OrphanQuestionInfo `json:"orphan_question,omitempty"`
	// NeedsPRInfo mirrors Issue.NeedsPRInfo for direct frontend access (#157).
	// Set when the execute session completed the work but could not push.
	NeedsPRInfo *types.NeedsPRInfo `json:"needs_pr_info,omitempty"`
}
