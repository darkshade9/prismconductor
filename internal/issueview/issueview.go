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

// IssueView is the single canonical read model for a board card. Assembled on
// the backend and emitted as bus.issue_view_updated on every change so the
// frontend can render without reconciling multiple stores.
type IssueView struct {
	Issue         types.Issue       `json:"issue"`
	LatestPlan    *types.Plan       `json:"latest_plan,omitempty"`
	ActiveSession *types.Session    `json:"active_session,omitempty"`
	PausedSession *types.Session    `json:"paused_session,omitempty"`
	LastFailure   *types.Session    `json:"last_failure,omitempty"`
	PoolBadge     *PoolBadge        `json:"pool_badge,omitempty"`
	DerivedColumn types.BoardColumn `json:"derived_column"`
}
