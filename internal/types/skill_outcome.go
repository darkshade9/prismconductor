package types

// SkillOutcome is one row in the skill_outcomes table. Written once per
// session at terminal state transition (completed, failed, blocked, needs_pr).
// PK is SessionID so an idempotent upsert is safe on duplicate delivery.
type SkillOutcome struct {
	SessionID      string `json:"session_id"`
	WorkspaceID    string `json:"workspace_id"`
	IssueNumber    int    `json:"issue_number"`
	SkillPath      string `json:"skill_path"`   // "bundled:conductor-plan" | abs repo path | ""
	SkillHash      string `json:"skill_hash"`   // sha256 hex at spawn time, or "fallback:harness"
	Mode           string `json:"mode"`         // "plan" | "execute"
	Outcome        string `json:"outcome"`      // "success"|"blocked"|"failed"|"needs_pr"
	BlockedReason  string `json:"blocked_reason"`
	UserAction     string `json:"user_action"`  // "" in phase A; filled by phase B
	CostCents      float64 `json:"cost_cents"`
	DurationMs     int64  `json:"duration_ms"`
	TranscriptPath string `json:"transcript_path"`
	CapturedAt     int64  `json:"captured_at"` // Unix epoch seconds
}

// SkillOutcomeSummary aggregates outcome counts for one skill path over a
// time window. Used by the curator skill to detect failure patterns.
type SkillOutcomeSummary struct {
	SkillPath    string         `json:"skill_path"`
	TotalSessions int           `json:"total_sessions"`
	Counts       map[string]int `json:"counts"` // outcome → count
}
