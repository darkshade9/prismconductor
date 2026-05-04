// Package types holds the cross-package data model defined in PRISMCONDUCTOR_PLAN.md §6.
package types

import "time"

// --- Workspace (§6.1) ---

type Workspace struct {
	ID            string          `json:"id"`
	DisplayName   string          `json:"display_name"`
	RepoPath      string          `json:"repo_path"`
	GitHubOwner   string          `json:"github_owner"`
	GitHubRepo    string          `json:"github_repo"`
	DefaultBranch string          `json:"default_branch"`
	Color         string          `json:"color"`
	AgentEnv      EnvSpec         `json:"agent_env"`
	SkillProfile  SkillProfile    `json:"skill_profile"`
	Conventions   ConventionHints `json:"conventions"`
	Enabled       bool            `json:"enabled"`
	AutoArchive   AutoArchive     `json:"auto_archive,omitempty"`
}

// AutoArchive configures per-workspace automatic archiving of DONE cards whose
// GitHub issue has been closed for longer than DaysClosed days (issue #107).
type AutoArchive struct {
	Enabled    bool `json:"enabled"`
	DaysClosed int  `json:"days_closed"` // default 7; minimum 1; maximum 365
}

type EnvSpec struct {
	EnvVars     map[string]string `json:"env_vars"`
	PreCommands []string          `json:"pre_commands"`
	Shell       string            `json:"shell"`
}

type SkillProfile struct {
	// Legacy fields kept for one-version migration window (issue #92).
	Mode                 SkillMode `json:"mode"`
	UseConductorPlan     bool      `json:"use_conductor_plan"`
	UseConductorExecute  bool      `json:"use_conductor_execute"`
	UseConductorClose    bool      `json:"use_conductor_close"`
	NativePlanCommand    string    `json:"native_plan_command"`
	NativeExecuteCommand string    `json:"native_execute_command"`
	NativeCloseCommand   string    `json:"native_close_command"`
	ExtraContextFiles    []string  `json:"extra_context_files"`
	AutoApplyLabels      *bool     `json:"auto_apply_labels,omitempty"`

	// PreferredPlanPoolID / PreferredWorkPoolID are per-workspace overrides
	// for role-based pool routing (issue #39). Empty means "use the
	// registry's default round-robin among role pools".
	PreferredPlanPoolID string `json:"preferred_plan_pool_id,omitempty"`
	PreferredWorkPoolID string `json:"preferred_work_pool_id,omitempty"`

	// PerStage holds per-stage skill overrides (issue #92). A missing key
	// falls through to the legacy Mode-based selection.
	PerStage map[string]SkillRef `json:"per_stage,omitempty"`
	// SkillsMigrated is set true after PerStage has been synthesized from
	// the legacy Mode/UseConductor* fields on first launch.
	SkillsMigrated bool `json:"skills_migrated,omitempty"`

	// AutoSelfHeal controls whether the system auto-spawns a Continue-Work
	// session when PR check runs fail (issue #116). Nil/absent means enabled
	// (default true) — the Self-heal button is always shown regardless.
	AutoSelfHeal *bool `json:"auto_self_heal,omitempty"`

	// SelfHealAttemptCap is the maximum number of consecutive auto-spawned
	// self-heal sessions per PR HEAD SHA. 0 means use the default (3).
	SelfHealAttemptCap int `json:"self_heal_attempt_cap,omitempty"`
}

// SkillForStage returns the SkillRef configured for the given stage, if any.
func (sp SkillProfile) SkillForStage(stage ConductorStage) (SkillRef, bool) {
	if sp.PerStage == nil {
		return SkillRef{}, false
	}
	ref, ok := sp.PerStage[string(stage)]
	return ref, ok
}

// AutoApplyLabelsEnabled returns true when the workspace opts in to auto-apply
// of planner-suggested labels. Absent (nil) means enabled — legacy workspace
// JSON keeps the new behavior without a migration.
func (sp SkillProfile) AutoApplyLabelsEnabled() bool {
	return sp.AutoApplyLabels == nil || *sp.AutoApplyLabels
}

type SkillMode string

const (
	SkillModeBundled SkillMode = "bundled"
	SkillModeHybrid  SkillMode = "hybrid"
	SkillModeNative  SkillMode = "native"
)

type ConventionHints struct {
	TestCommand    string `json:"test_command"`
	BuildCommand   string `json:"build_command"`
	LintCommand    string `json:"lint_command"`
	PyEnvPath      string `json:"py_env_path"`
	PackageManager string `json:"package_manager"`
}

// --- Pool (heterogeneous worker fleets, §6.6 / issue #27, role added in #39) ---

type Pool struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	Provider  Provider  `json:"provider"`
	Endpoint  string    `json:"endpoint"`
	Model     string    `json:"model"`
	Capacity  int       `json:"capacity"`
	Enabled   bool      `json:"enabled"`
	APIKey    string    `json:"api_key,omitempty"`
	Role      Role      `json:"role"`
	CreatedAt time.Time `json:"created_at"`
	// Priority controls pool selection order within a role group (issue #40).
	// Lower value = higher preference. Default 0. Pools with equal priority
	// are tried in ascending created_at order.
	Priority int `json:"priority"`
	// Temperature, when non-nil, is sent verbatim in the outgoing chat-completions
	// request. When nil the field is omitted and the provider uses its default.
	// This unblocks models that reject an explicit temperature (e.g. gpt-5-codex).
	Temperature *float64 `json:"temperature,omitempty"`
	// Per-pool harness budget overrides (issue #89). Nil = use harness.DefaultBudget() value.
	MaxTurns       *int           `json:"max_turns,omitempty"`
	MaxInputTokens *int           `json:"max_input_tokens,omitempty"`
	BashTimeout    *time.Duration `json:"bash_timeout,omitempty"`
	OutputCap      *int           `json:"output_cap,omitempty"`
	// Scope controls whether this pool is shared across all workspaces or
	// dedicated to a specific workspace (issue #109). Empty = shared.
	Scope       PoolScope `json:"scope,omitempty"`
	WorkspaceID string    `json:"workspace_id,omitempty"`
}

// PendingPoolRequest is a persisted "waiting for pool" request. Created when
// a spawn attempt finds no free slot; drained when a slot frees (issue #40).
type PendingPoolRequest struct {
	ID          int64     `json:"id"`
	WorkspaceID string    `json:"workspace_id"`
	IssueNumber int       `json:"issue_number"`
	Role        Role      `json:"role"`
	Action      string    `json:"action"`
	CreatedAt   time.Time `json:"created_at"`
}

// PoolScope controls whether a pool is reserved for one workspace or shared
// across all workspaces (issue #109). Empty string is treated as PoolScopeShared
// for backwards compatibility with rows written before this field existed.
type PoolScope string

const (
	PoolScopeShared    PoolScope = "shared"
	PoolScopeWorkspace PoolScope = "workspace"
)

// Role names which step in the orchestration loop a pool serves (issue #39).
// Stored as TEXT in the pools table with a CHECK constraint.
type Role string

const (
	RolePlan         Role = "plan"
	RoleWork         Role = "work"
	RoleOrchestrator Role = "orchestrator"
)

// ValidRole reports whether r is one of the three known roles.
func ValidRole(r Role) bool {
	switch r {
	case RolePlan, RoleWork, RoleOrchestrator:
		return true
	}
	return false
}

type Provider string

const (
	ProviderClaude   Provider = "claude"
	ProviderOpenAI   Provider = "openai"
	ProviderLiteLLM  Provider = "litellm"
	ProviderLMStudio Provider = "lmstudio"
	ProviderOllama   Provider = "ollama"
	// ProviderGemini routes through mozilla-ai/any-llm-go's native Gemini
	// client. Sits alongside the OpenAI-compat providers — and is preferred
	// over routing Gemini through ProviderOpenAI's /v1beta/openai shim because
	// the native API can round-trip `thought_signature` for thinking-tier
	// models. First trial of the any-llm-go integration.
	ProviderGemini Provider = "gemini"
)

// --- Goal (§6.2) ---

type Goal struct {
	ID             string     `json:"id"`
	WorkspaceID    string     `json:"workspace_id"`
	Title          string     `json:"title"`
	Intent         string     `json:"intent"`
	AcceptanceRule string     `json:"acceptance_rule"`
	IssueFilter    IssueQuery `json:"issue_filter"`
	Status         GoalStatus `json:"status"`
	Order          int        `json:"order"`
	CreatedAt      time.Time  `json:"created_at"`
	AchievedAt     *time.Time `json:"achieved_at,omitempty"`
	Notes          string     `json:"notes"`
}

type IssueQuery struct {
	Labels    []string `json:"labels"`
	Milestone string   `json:"milestone"`
	FreeText  string   `json:"free_text"`
	Includes  []int    `json:"includes"`
	Excludes  []int    `json:"excludes"`
}

type GoalStatus string

const (
	GoalBacklog   GoalStatus = "backlog"
	GoalActive    GoalStatus = "active"
	GoalAchieved  GoalStatus = "achieved"
	GoalAbandoned GoalStatus = "abandoned"
)

// --- Issue (§6.3) ---

type Issue struct {
	Number      int       `json:"number"`
	WorkspaceID string    `json:"workspace_id"`
	Title       string    `json:"title"`
	Body        string    `json:"body"`
	Labels      []string  `json:"labels"`
	State       string    `json:"state"`
	URL         string    `json:"url"`
	UpdatedAt   time.Time `json:"updated_at"`

	GoalID       *string     `json:"goal_id,omitempty"`
	Priority     float64     `json:"priority"`
	Dependencies []int       `json:"dependencies"`
	DepRationale string      `json:"dep_rationale"`
	Column       BoardColumn `json:"column"`
	Plan         *Plan       `json:"plan,omitempty"`
	SessionID    *string     `json:"session_id,omitempty"`
	LastError    string      `json:"last_error,omitempty"`
	PRNumber     *int        `json:"pr_number,omitempty"`
	PRURL        string      `json:"pr_url,omitempty"`
	ArchivedAt   *time.Time  `json:"archived_at,omitempty"`
	ClosedAt     *time.Time  `json:"closed_at,omitempty"`
	// WaitingForPool is set true when a spawn attempt was queued because no
	// pool had free capacity (issue #40). Cleared on successful drain.
	WaitingForPool bool `json:"waiting_for_pool,omitempty"`

	// WorkSeconds is the cumulative active work time (plan + execute sessions)
	// accumulated from completed sessions. Updated atomically when a session
	// ends; preserved across GitHub poll re-saves (issue #46).
	WorkSeconds        int64 `json:"work_seconds,omitempty"`
	WorkSecondsPlan    int64 `json:"work_seconds_plan,omitempty"`
	WorkSecondsExecute int64 `json:"work_seconds_execute,omitempty"`

	// CostUSD is the cumulative LLM spend across all sessions for this issue
	// (issue #47). Accumulated from Claude stream-json result events; preserved
	// across GitHub poll re-saves like WorkSeconds.
	CostUSD float64 `json:"cost_usd,omitempty"`
}

type BoardColumn string

const (
	ColTodo       BoardColumn = "todo"
	ColPlan       BoardColumn = "plan"
	ColInProgress BoardColumn = "in_progress"
	ColReview     BoardColumn = "review"
	ColDone       BoardColumn = "done"
)

// --- Plan (§6.4) ---

type Plan struct {
	IssueNumber          int          `json:"issue_number"`
	WorkspaceID          string       `json:"workspace_id"`
	Revision             int          `json:"revision"`
	GoalSummary          string       `json:"goal_summary,omitempty"`
	ExecutiveSummary     string       `json:"executive_summary,omitempty"`
	PlanMarkdown         string       `json:"plan_markdown"`
	FilesToModify        []FileIntent `json:"files_to_modify"`
	DependenciesDetected []int        `json:"dependencies_detected"`
	SuggestedLabels      []string     `json:"suggested_labels,omitempty"`
	Questions            []Question   `json:"questions"`
	EstimatedComplexity  string       `json:"estimated_complexity"`
	ReadyToExecute       bool         `json:"ready_to_execute"`
	GeneratedAt          time.Time    `json:"generated_at"`
	ApprovedAt           *time.Time   `json:"approved_at,omitempty"`
}

// Label is a GitHub repo label mirrored locally.
type Label struct {
	Name        string `json:"name"`
	Color       string `json:"color"` // 6 lowercase hex chars, no leading '#'
	Description string `json:"description"`
}

type FileIntent struct {
	Path   string `json:"path"`
	Intent string `json:"intent"`
}

type Question struct {
	ID       string       `json:"id"`
	Type     QuestionType `json:"type"`
	Prompt   string       `json:"prompt"`
	Options  []string     `json:"options,omitempty"`
	Default  *string      `json:"default,omitempty"`
	Required bool         `json:"required"`
	Answer   *string      `json:"answer,omitempty"`
}

type QuestionType string

const (
	QuestionSingleChoice QuestionType = "single_choice"
	QuestionMultiChoice  QuestionType = "multi_choice"
	QuestionFreeText     QuestionType = "free_text"
	QuestionYesNo        QuestionType = "yes_no"
)

// --- Session (§6.5) ---

type Session struct {
	ID                string       `json:"id"`
	WorkspaceID       string       `json:"workspace_id"`
	IssueNumber       int          `json:"issue_number"`
	Mode              SessionMode  `json:"mode"`
	State             SessionState `json:"state"`
	StartedAt         time.Time    `json:"started_at"`
	EndedAt           *time.Time   `json:"ended_at,omitempty"`
	PID               int          `json:"pid"`
	Transcript        string       `json:"-"`
	LastPrompt        string       `json:"last_prompt"`
	BlockedReason     string       `json:"blocked_reason,omitempty"`
	PendingQuestionID string       `json:"pending_question_id,omitempty"`

	// PoolID identifies which worker pool slot this session reserved.
	// Persisted so reattach can release the right slot if the conductor
	// goes down mid-session — without it the in-memory active count drifts
	// every restart and eventually saturates at capacity. Empty for legacy
	// rows spawned before the field was introduced.
	PoolID string `json:"pool_id,omitempty"`

	// AcknowledgedAt is set (Unix epoch) when the user explicitly clears a
	// blocked/failed session via ClearIssueFailure or by dragging the card to
	// TODO/PLAN. The session row is preserved for audit; the UI ignores
	// acknowledged sessions when computing lastFailure/activeSession overlays.
	AcknowledgedAt *int64 `json:"acknowledged_at,omitempty"`

	// TranscriptOffset is the byte offset into the transcript file of the
	// last fully-processed line (issue #54). Lives on the sessions column,
	// excluded from the JSON blob to keep external snapshots schema-stable.
	TranscriptOffset int64 `json:"-"`

	// Issue #101: per-session token counts and estimated cost persisted at
	// session end. Claude sessions get these from the result event; harness
	// sessions from the budgetState accumulator.
	InputTokens        int64   `json:"input_tokens,omitempty"`
	OutputTokens       int64   `json:"output_tokens,omitempty"`
	EstimatedCostCents float64 `json:"estimated_cost_cents,omitempty"`
}

// MidRunAnswer is the §6.4-shaped answer payload for a mid-run question
// (issue #17). One question per call; symmetry with the multi-question
// AnswerSubmission shape is intentionally not preserved.
type MidRunAnswer struct {
	QuestionID string   `json:"question_id"`
	Answer     string   `json:"answer"`
	Multi      []string `json:"multi,omitempty"`
}

// ActivityEntry is one captured tool-call event stored in the per-session
// ring buffer (issue #99).
type ActivityEntry struct {
	ToolName    string    `json:"tool_name"`
	ArgsSummary string    `json:"args_summary"` // ≤50 chars, newlines escaped
	ArgsJSON    string    `json:"args_json"`    // capped at 2048 chars for popover
	At          time.Time `json:"at"`
}

// SessionActivity is the per-tick liveness payload emitted on the
// "session.activity" Wails event. Lets the UI show "still alive, doing X"
// without subscribing to every PTY line.
type SessionActivity struct {
	SessionID    string    `json:"session_id"`
	WorkspaceID  string    `json:"workspace_id"`
	IssueNumber  int       `json:"issue_number"`
	ToolCount    int       `json:"tool_count"`
	LastAction   string    `json:"last_action"`
	LastActionAt time.Time `json:"last_action_at"`
	// Issue #99: activity strip fields.
	LastToolArgsSummary string          `json:"last_tool_args_summary,omitempty"`
	Tail                []ActivityEntry `json:"tail,omitempty"`
}

type SessionMode string

const (
	ModePlan    SessionMode = "plan"
	ModeExecute SessionMode = "execute"
)

type SessionState string

const (
	StateRunning            SessionState = "running"
	StateWaitingForInput    SessionState = "waiting_for_input"
	StateBlocked            SessionState = "blocked"
	StateCompleted          SessionState = "completed"
	StateFailed             SessionState = "failed"
	StatePausedForQuestion  SessionState = "paused_for_question"
)

// --- Pool usage / rate limits (issue #52) ---

// PoolUsage is a last-seen snapshot of rate-limit consumption for one
// (pool, window) pair. Persisted in pool_usage; the primary key is
// (pool_id, window) so every upsert overwrites the prior snapshot.
type PoolUsage struct {
	PoolID     string    `json:"pool_id"`
	PoolName   string    `json:"pool_name"`
	Provider   string    `json:"provider"`
	Window     string    `json:"window"`
	LimitValue int64     `json:"limit_value"`
	Used       int64     `json:"used"`
	ResetsAt   time.Time `json:"resets_at"`
	CapturedAt time.Time `json:"captured_at"`
}
