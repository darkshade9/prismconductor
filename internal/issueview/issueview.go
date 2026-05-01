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
	LastSession      *types.Session    `json:"last_session,omitempty"`
	PoolBadge        *PoolBadge        `json:"pool_badge,omitempty"`
	DerivedColumn    types.BoardColumn `json:"derived_column"`
	TestsFailingInfo *TestsFailingInfo `json:"tests_failing_info,omitempty"`
}
