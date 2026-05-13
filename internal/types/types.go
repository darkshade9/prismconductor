// Package types holds the cross-package data model defined in PRISMCONDUCTOR_PLAN.md §6.
package types

import (
	"encoding/json"
	"errors"
	"time"
)

// --- Workspace (§6.1) ---

// ExecutionTarget controls where agent work runs for this workspace.
// "local" (default) runs on the user's machine; "remote" runs inside a
// Cloudflare Worker deployed on the user's CF account (issue #171).
type ExecutionTarget string

const (
	ExecutionTargetLocal  ExecutionTarget = "local"
	ExecutionTargetRemote ExecutionTarget = "remote"
)

// RemoteSecretRefs holds references (names, not values) into the Cloudflare
// Secrets store for a remote workspace. Only the names are persisted locally;
// the raw token values live exclusively in the user's CF account.
type RemoteSecretRefs struct {
	GitHubPATRef       string `json:"github_pat_ref"`          // CF Secret name, e.g. "GITHUB_PAT"
	CFAPITokenRef      string `json:"cf_api_token_ref"`        // CF Secret name, e.g. "CF_API_TOKEN"
	ConductorAPIKeyRef string `json:"conductor_api_key_ref"`   // CF Secret name, e.g. "CONDUCTOR_API_KEY"
}

// RemoteConfig holds the Cloudflare configuration for a remote workspace.
// Fields are populated by DeployRemoteWorker during workspace setup and stored
// in workspaces.json. Never store raw token values here — use RemoteSecretRefs.
type RemoteConfig struct {
	// CFAccountID is the user's Cloudflare account ID, captured once during
	// TestCloudflareToken and reused for all subsequent CF API calls.
	CFAccountID string `json:"cf_account_id"`
	// CFWorkerName is the deployed CF Worker script name (e.g. "prismconductor-<wsID>").
	CFWorkerName string `json:"cf_worker_name"`
	// CFWorkerEndpointURL is the base URL of the deployed worker
	// (e.g. "https://prismconductor-abc.workers.dev").
	CFWorkerEndpointURL string `json:"cf_worker_endpoint_url"`
	// CFDeploymentVersion is the etag / version tag returned by the CF API on
	// the most recent deploy. Used to detect stale deployments.
	CFDeploymentVersion string `json:"cf_deployment_version,omitempty"`
	// SecretRefs holds the names (not values) of CF Secrets created during setup.
	SecretRefs RemoteSecretRefs `json:"cf_secret_refs"`
	// TokenExpired is set true when the conductor detects a 401 from the remote
	// worker. Cleared when the user re-deploys via Settings → Workspace → Auth.
	TokenExpired bool `json:"token_expired,omitempty"`
	// RemoteUnreachable is set true when the startup probe cannot reach the
	// CFWorkerEndpointURL. Cleared on next successful probe or redeploy.
	RemoteUnreachable bool `json:"remote_unreachable,omitempty"`
}

// SigningStrategy controls how the conductor-execute worker attempts to commit
// and push work on behalf of a workspace (issue #175).
type SigningStrategy string

const (
	// SigningStrategyAuto tries Tier 1 (GitHub API), then Tier 2 (local signing),
	// then Tier 3 (manual NEEDS_PR). This is the default when unset.
	SigningStrategyAuto      SigningStrategy = "auto"
	SigningStrategyGitHubAPI SigningStrategy = "github_api"
	SigningStrategyLocal     SigningStrategy = "local"
	SigningStrategyManual    SigningStrategy = "manual"
)

// ComplexityScale selects which estimation vocabulary the planner emits for
// a workspace (issue #233). Empty string is equivalent to ComplexityScaleTShirt.
type ComplexityScale string

const (
	ComplexityScaleTShirt   ComplexityScale = "tshirt"
	ComplexityScaleFibonacci ComplexityScale = "fibonacci"
	ComplexityScaleLinear10  ComplexityScale = "linear10"
)
type Workspace struct {
	ID              string          `json:"id"`
	DisplayName     string          `json:"display_name"`
	RepoPath        string          `json:"repo_path"`
	GitHubOwner     string          `json:"github_owner"`
	GitHubRepo      string          `json:"github_repo"`
	DefaultBranch   string          `json:"default_branch"`
	Color           string          `json:"color"`
	AgentEnv        EnvSpec         `json:"agent_env"`
	SkillProfile    SkillProfile    `json:"skill_profile"`
	Conventions     ConventionHints `json:"conventions"`
	Enabled         bool            `json:"enabled"`
	AutoArchive     AutoArchive     `json:"auto_archive,omitempty"`
	// SigningStrategy controls the commit+push tier used by execute workers.
	// Empty string is equivalent to SigningStrategyAuto (issue #175).
	SigningStrategy SigningStrategy `json:"signing_strategy,omitempty"`
	// Pipeline holds the custom pipeline configuration for this workspace.
	// Nil means "use the default Plan→Execute→Close flow" (issue #146).
	Pipeline *WorkspacePipeline `json:"pipeline,omitempty"`
	// ExecutionTarget controls where agent work runs (issue #171).
	// Absent / empty means "local" for backward compatibility.
	ExecutionTarget ExecutionTarget `json:"execution_target,omitempty"`
	// RemoteConfig holds Cloudflare configuration for remote workspaces.
	// Nil for local workspaces.
	RemoteConfig *RemoteConfig `json:"remote_config,omitempty"`
	// Provisioning is true while a remote workspace is mid-creation (deploy
	// started but not yet fully committed). Rows stuck in this state longer
	// than ~10 minutes are cleaned up by the startup reconciler (issue #192).
	Provisioning   bool       `json:"provisioning,omitempty"`
	ProvisioningAt *time.Time `json:"provisioning_at,omitempty"`

	// RetryPolicy configures auto-retry behaviour for execute sessions in this
	// workspace. Nil means use DefaultRetryPolicy (#221).
	RetryPolicy *RetryPolicy `json:"retry_policy,omitempty"`

	// ComplexityScale selects the estimation vocabulary for this workspace's
	// planner output. Empty means tshirt (default). See internal/complexity/scales.go.
	ComplexityScale ComplexityScale `json:"complexity_scale,omitempty"`

	// TrackerKind identifies the issue-tracker backend for this workspace.
	// Empty / absent means "github" for backward compatibility (issue #289).
	// Valid values: "github", "jira".
	TrackerKind string `json:"tracker_kind,omitempty"`

	// TrackerConfig holds tracker-specific configuration as a JSON blob.
	// For kind="jira" this is jira.JiraConfig. Nil for GitHub workspaces.
	TrackerConfig json.RawMessage `json:"tracker_config,omitempty"`
}

// WorkspacePipeline is the per-workspace pipeline configuration (issue #146).
// Steps define a DAG of work items that run after the core Plan+Execute cycle.
// A nil pipeline means use the default Execute→Close behavior.
type WorkspacePipeline struct {
	Steps   []PipelineStep `json:"steps"`
	Version int            `json:"version"` // incremented on each save
}

// PipelineStep is one node in the pipeline DAG (issue #146).
type PipelineStep struct {
	ID        string   `json:"id"`
	Name      string   `json:"name"`
	SkillRef  SkillRef `json:"skill_ref"`
	// AutoChain controls whether the next step spawns automatically on success.
	// false (default) = gated: user must click Continue before the next step spawns.
	AutoChain bool `json:"auto_chain"`
	// MaxLoops is the maximum number of times this step may run for a single card.
	// 0 means the step is not a loop target. Steps in a cycle must have MaxLoops > 0.
	MaxLoops  int    `json:"max_loops"`
	OnSuccess string `json:"on_success"` // step ID of next step on PASS; "" = done
	OnFail    string `json:"on_fail"`    // step ID of next step on FAIL; "" = stop/BLOCKED
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

	// AutoContinueOnComment controls whether "Comment & Request Fix" auto-spawns
	// a Continue Work session without a confirmation dialog (issue #159).
	// Nil/absent means enabled (default true).
	AutoContinueOnComment *bool `json:"auto_continue_on_comment,omitempty"`

	// NotifyOnBotComments controls whether comments authored by known bot
	// accounts surface as unread New Comment badges (issue #159).
	// Nil/absent means disabled (default false).
	NotifyOnBotComments *bool `json:"notify_on_bot_comments,omitempty"`
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

// --- ProviderEntity (issue #268: centralized LLM credentials) ---

// ProviderEntity is a persisted set of LLM credentials shared across pools.
// Distinct from the Provider enum (which names a driver kind like "claude").
type ProviderEntity struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	Kind      Provider  `json:"kind"`
	Endpoint  string    `json:"endpoint"`
	APIKey    string    `json:"api_key,omitempty"`
	CreatedAt time.Time `json:"created_at"`
}

// --- Pool (heterogeneous worker fleets, §6.6 / issue #27, role added in #39) ---

type Pool struct {
	ID         string    `json:"id"`
	Name       string    `json:"name"`
	ProviderID string    `json:"provider_id,omitempty"`
	Provider   Provider  `json:"provider"`
	Endpoint   string    `json:"endpoint"`
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
	// RoleArchitect marks a pool as a short-lived peer-agent consultant.
	// When an implementer emits a QUESTION_PENDING with audience="peer_agent",
	// the conductor spawns one architect worker to answer it automatically
	// instead of surfacing the question to the human user (issue #211).
	RoleArchitect Role = "architect"
)

// ValidRole reports whether r is one of the known roles.
func ValidRole(r Role) bool {
	switch r {
	case RolePlan, RoleWork, RoleOrchestrator, RoleArchitect:
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
	// ProviderCodex routes inference through the local `codex` CLI binary
	// (OpenAI's agent CLI) using the same PTY subprocess pattern as Claude.
	// Auth is managed by the codex CLI itself (browser OAuth against a
	// ChatGPT subscription); no API key is stored in the conductor.
	// Pool capacity defaults to 1 — subscription limits are per-account.
	ProviderCodex Provider = "codex"
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

// --- IssueDep (§6.3, issue #210) ---

// IssueDep identifies a dependency on a GitHub issue, optionally in another
// workspace. WorkspaceID == "" means same workspace (legacy same-workspace
// behavior). WorkspaceID is the short workspace key (e.g. "infra", "app").
type IssueDep struct {
	WorkspaceID string `json:"workspace_id"`
	Number      int    `json:"number"`
}

// IssueDepList is a slice of IssueDep that unmarshals transparently from the
// legacy []int format (same-workspace deps written before issue #210) as well
// as the new []IssueDep format.
type IssueDepList []IssueDep

func (d *IssueDepList) UnmarshalJSON(b []byte) error {
	// New canonical format: [{workspace_id,number}…]
	var deps []IssueDep
	if err := json.Unmarshal(b, &deps); err == nil {
		*d = deps
		return nil
	}
	// Legacy format: [1, 2, 3] — treat as same-workspace deps.
	var ints []int
	if err := json.Unmarshal(b, &ints); err != nil {
		return err
	}
	out := make(IssueDepList, len(ints))
	for i, n := range ints {
		out[i] = IssueDep{Number: n}
	}
	*d = out
	return nil
}

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

	GoalID       *string      `json:"goal_id,omitempty"`
	Priority     float64      `json:"priority"`
	Dependencies IssueDepList `json:"dependencies"`
	DepRationale string       `json:"dep_rationale"`
	// CrossDeps lists cross-workspace dependencies (Phase B of issue #208).
	// An issue with unresolved CrossDeps is blocked from being pulled to PLAN.
	CrossDeps []IssueDep `json:"cross_deps,omitempty"`
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

	// WaitingForDep is set to the first unresolved cross-workspace dependency
	// for this issue. Non-nil when the issue is in REVIEW but a referenced
	// dep in another workspace is still open (issue #210). Cleared by the
	// GitHub poller when the dep closes/merges.
	WaitingForDep *IssueDep `json:"waiting_for_dep,omitempty"`

	// Pipeline tracking fields (issue #146). Stamped at first pipeline-step
	// spawn so in-flight cards continue on the saved pipeline version.
	PipelineStepID  string         `json:"pipeline_step_id,omitempty"`
	PipelineLoops   map[string]int `json:"pipeline_loops,omitempty"`
	PipelineVersion int            `json:"pipeline_version,omitempty"`

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

	// NeedsPRInfo is set when an execute session completed the work but could
	// not push (signing failure, auth error, branch protection, etc.). The UI
	// renders a "manual push needed" badge; the poller polls GitHub for any PR
	// opened against NeedsPRInfo.Branch and auto-attaches it (#157).
	// Cleared by MarkPROpened.
	NeedsPRInfo *NeedsPRInfo `json:"needs_pr_info,omitempty"`

	// FailureReason is a concise human-readable explanation of why the most
	// recent execute session failed. Set when an execute session reaches
	// StateBlocked or StateFailed; cleared on ClearIssueFailure (#194).
	FailureReason string `json:"failure_reason,omitempty"`

	// TrackerRef holds the canonical tracker-agnostic reference for this issue
	// (issue #289). Nil for legacy GitHub issues where the numeric Number field
	// is sufficient; set by Jira / Linear / GitLab pollers.
	TrackerRef *TrackerRef `json:"tracker_ref,omitempty"`

	// DependsOnExternal is set when this issue was filed by the fan-out flow as
	// a follow-on to a PR in another workspace (issue #297). Non-nil issues are
	// held back from auto-pull until Resolved=true (set by the poller on merge).
	DependsOnExternal *ExternalDep `json:"depends_on_external,omitempty"`
}

// TrackerRef is a canonical, tracker-agnostic identifier for a single issue.
// For GitHub issues the Identifier is "owner/repo#<num>". For Jira it is the
// issue key, e.g. "PROJ-123".
type TrackerRef struct {
	Kind       string `json:"kind"`       // "github" | "jira"
	Identifier string `json:"identifier"` // human-readable key used in branch names
}

// NeedsPRInfo holds the context needed to display the NEEDS_PR badge and poll
// GitHub for a matching PR (#157).
type NeedsPRInfo struct {
	Branch      string `json:"branch"`
	WorktreeDir string `json:"worktree_dir"`
	Reason      string `json:"reason"`
	// Kind is "commit_signing" when the commit itself failed, or "push" when
	// commits landed locally but the remote push was rejected.
	Kind string `json:"kind"`
	// CommitMsgFile is the path to the worker-drafted commit message file
	// written by Tier 3 of the three-tier commit fallback (issue #175).
	// Empty when the commit landed but the push failed.
	CommitMsgFile string `json:"commit_msg_file,omitempty"`
	// CommitMsg is the first 500 bytes of CommitMsgFile content for inline
	// display in the UI. Empty when CommitMsgFile is empty.
	CommitMsg string `json:"commit_msg,omitempty"`
}

type BoardColumn string

const (
	ColTodo       BoardColumn = "todo"
	ColPlan       BoardColumn = "plan"
	ColInProgress BoardColumn = "in_progress"
	// ColBlocked is the repair column for issues whose execute session died
	// mid-flow without opening a PR. Cards sit here until the user acknowledges
	// the failure or opens the orphan PR (#194).
	ColBlocked BoardColumn = "blocked"
	ColReview  BoardColumn = "review"
	ColDone    BoardColumn = "done"
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
	// CrossDepsDetected lists cross-workspace dependencies detected by the planner
	// (Phase B of issue #208). Kept separate from DependenciesDetected for
	// backward compatibility.
	CrossDepsDetected []IssueDep `json:"cross_deps_detected,omitempty"`
	SuggestedLabels   []string   `json:"suggested_labels,omitempty"`
	Questions         []Question `json:"questions"`
	EstimatedComplexity string     `json:"estimated_complexity"`
	ReadyToExecute      bool       `json:"ready_to_execute"`
	GeneratedAt         time.Time  `json:"generated_at"`
	ApprovedAt          *time.Time `json:"approved_at,omitempty"`
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
	// Audience controls who receives this question. "" or "user" means surface
	// to the human; "peer_agent" means route to an architect-pool worker.
	Audience string `json:"audience,omitempty"`
}

type QuestionType string

const (
	QuestionSingleChoice QuestionType = "single_choice"
	QuestionMultiChoice  QuestionType = "multi_choice"
	QuestionFreeText     QuestionType = "free_text"
	QuestionYesNo        QuestionType = "yes_no"
)

// FailureKindQuotaExceeded is the FailureCause.Kind value for provider
// daily/monthly quota exhaustion (e.g. Gemini free-tier RESOURCE_EXHAUSTED).
// The retry scheduler treats this as retryable with a long backoff anchored to
// RetryAfter rather than exponential backoff.
const FailureKindQuotaExceeded = "quota_exceeded"

// FailureCause is a structured record of why a session reached a terminal
// failed or blocked state. Captured at the moment the terminal sentinel is
// written so that the UI can always surface a human-readable reason (#30).
type FailureCause struct {
	// Kind classifies the failure origin. Known values:
	//   "blocked"                  — worker emitted BLOCKED: sentinel
	//   "exit_code"                — process exited with non-zero code
	//   "signal"                   — process terminated by signal
	//   "quota_exceeded"           — provider quota exhausted (see llm.QuotaExceededError)
	//   "unknown_pre_card_state_v2" — legacy row backfilled at startup
	Kind     string `json:"kind"`
	Reason   string `json:"reason,omitempty"`
	ExitCode int    `json:"exit_code,omitempty"`
	Signal   string `json:"signal,omitempty"`
	// RetryAfter, when non-nil, indicates the earliest time the scheduler
	// should retry. Set for quota_exceeded failures when the provider
	// advertises a reset time. Nil means use the standard backoff formula.
	RetryAfter *time.Time `json:"retry_after,omitempty"`
}

// RetryPolicy configures the automatic-retry behaviour for execute sessions
// (issue #221). A nil workspace RetryPolicy falls back to DefaultRetryPolicy.
type RetryPolicy struct {
	MaxAttempts int           `json:"max_attempts"`
	BaseBackoff time.Duration `json:"base_backoff"`
	MaxBackoff  time.Duration `json:"max_backoff"`
	JitterPct   int           `json:"jitter_pct"`
}

// DefaultRetryPolicy is the conservative retry policy used when a workspace
// has no explicit policy configured.
var DefaultRetryPolicy = RetryPolicy{
	MaxAttempts: 3,
	BaseBackoff: 10 * time.Second,
	MaxBackoff:  5 * time.Minute,
	JitterPct:   25,
}

// GlobalRetryMaxAttempts and GlobalRetryMaxWindow are hard caps that override
// any workspace-level policy (issue #221).
const GlobalRetryMaxAttempts = 10
const GlobalRetryMaxWindow = time.Hour

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

	// PipelineStepName is non-empty when this session is running a custom
	// pipeline step (issue #146). Used by the card StatusRow to display the
	// active step name instead of the generic "working" label.
	PipelineStepName string `json:"pipeline_step_name,omitempty"`

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

	// Issue #194: diagnostics captured at subprocess exit for failed execute sessions.
	ExitCode        *int   `json:"exit_code,omitempty"`
	Signal          string `json:"signal,omitempty"`
	CmdWaitTimeMs   int64  `json:"cmd_wait_time_ms,omitempty"`
	LastStderrChunk string `json:"last_stderr_chunk,omitempty"`

	// Branch is the git branch this execute session ran on (#194).
	Branch string `json:"branch,omitempty"`

	// FailureCause is set when the session reaches a terminal failed/blocked
	// state. Nil for completed, paused, and needs_pr sessions (#30).
	FailureCause *FailureCause `json:"failure_cause,omitempty"`

	// RetryAttempt is the 1-based retry index when this session was spawned
	// automatically by the retry scheduler. 0 for first-attempt sessions (#221).
	RetryAttempt int `json:"retry_attempt,omitempty"`
	// RetryOfSessionID is the ID of the session whose transient failure triggered
	// this session. Empty for first-attempt sessions (#221).
	RetryOfSessionID string `json:"retry_of_session_id,omitempty"`

	// Issue #293: skill identity captured at spawn time for outcome logging.
	// SkillPath is "bundled:<name>", an absolute repo path, or "" (harness fallback).
	// SkillHash is sha256 hex of the skill markdown at spawn time, or "fallback:harness".
	// Both omitempty so old session JSON deserializes cleanly.
	SkillPath string `json:"skill_path,omitempty"`
	SkillHash string `json:"skill_hash,omitempty"`
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
	StateRunning           SessionState = "running"
	StateWaitingForInput   SessionState = "waiting_for_input"
	StateBlocked           SessionState = "blocked"
	StateCompleted         SessionState = "completed"
	StateFailed            SessionState = "failed"
	StatePausedForQuestion SessionState = "paused_for_question"
	// StateNeedsPR is a terminal state for execute sessions that completed the
	// work (code + commit) but could not push due to signing/auth/protection
	// rules. The card moves to REVIEW with NeedsPRInfo on the Issue (#157).
	StateNeedsPR SessionState = "needs_pr"
)

// --- Agent terminal sessions (issue #161) ---

// AgentInfo describes a CLI agent binary discovered on PATH.
type AgentInfo struct {
	Name   string `json:"name"`   // display name, e.g. "claude"
	Binary string `json:"binary"` // absolute path to the executable
}

// AgentTermSession is the runtime state of an ephemeral PTY session.
// Sessions are never persisted to disk or the database.
type AgentTermSession struct {
	WorkspaceID string `json:"workspace_id"`
	SessionID   string `json:"session_id"`
	AgentBin    string `json:"agent_bin"` // display name of the agent binary
	PID         int    `json:"pid"`
	Cwd         string `json:"cwd"` // spawn working directory (workspace RepoPath)
}

// --- PR comments (issue #159) ---

// PRCommentKind distinguishes conversation comments (on the issue thread)
// from line-level review comments (attached to a diff hunk).
type PRCommentKind string

const (
	PRCommentKindConversation PRCommentKind = "conversation"
	PRCommentKindReview       PRCommentKind = "review"
)

// PRComment is a single GitHub PR comment, persisted in pr_comments.
// Conversation comments come from issues/:num/comments; review comments
// come from pulls/:num/comments (inline diff comments).
type PRComment struct {
	WorkspaceID string        `json:"workspace_id"`
	IssueNumber int           `json:"issue_number"`
	CommentID   int64         `json:"comment_id"`
	Author      string        `json:"author"`
	Body        string        `json:"body"`
	Kind        PRCommentKind `json:"kind"`
	FilePath    string        `json:"file_path,omitempty"`
	LineNumber  int           `json:"line_number,omitempty"`
	CreatedAt   time.Time     `json:"created_at"`
	ReadAt      *time.Time    `json:"read_at,omitempty"`
	PendingPost bool          `json:"pending_post,omitempty"`
}

// --- Collection (issue #209, Phase A of #208) ---

// Collection groups several Workspace records under a shared name and shared
// context document. Workers spawned for member workspaces receive sibling repo
// paths (Bundled mode) and the collection's ContextMD prepended to their prompt.
type Collection struct {
	ID           string    `json:"id"`
	Name         string    `json:"name"`
	WorkspaceIDs []string  `json:"workspace_ids"`
	ContextMD    string    `json:"context_md"`
	CreatedAt    time.Time `json:"created_at"`
	UpdatedAt    time.Time `json:"updated_at"`
}

// ErrAlreadyInCollection is returned by AddWorkspaceToCollection when the
// workspace is already a member of any collection (v1 single-membership rule).
var ErrAlreadyInCollection = errors.New("workspace already belongs to a collection")

// --- Session Ledger (issue #298) ---

// SessionLedgerRow is one entry in the per-issue session ledger. Derived at
// read time by BuildSessionLedger in the issueview assembler; no DB migration
// required. Model/provider reflect the pool's current configuration — pools
// edited after a session ran will show their new values here (resolve-on-read).
type SessionLedgerRow struct {
	SessionID    string        `json:"session_id"`
	Role         string        `json:"role"`          // "plan", "execute", "pipeline:<step>"
	PoolID       string        `json:"pool_id"`
	PoolName     string        `json:"pool_name"`     // "(pool deleted)" if lookup fails
	Provider     string        `json:"provider"`
	Model        string        `json:"model"`
	StartedAt    time.Time     `json:"started_at"`
	DurationS    float64       `json:"duration_s"`    // 0 if session never ended
	CostUSD      float64       `json:"cost_usd"`
	InputTokens  int64         `json:"input_tokens"`
	OutputTokens int64         `json:"output_tokens"`
	Outcome      string        `json:"outcome"`       // mirrors SessionState values
	FailureCause *FailureCause `json:"failure_cause,omitempty"`
}

// --- Fan-out (issue #297) ---

// ExternalDep records that this issue was filed as a follow-on to a PR in
// another workspace. The issue is held back from auto-pull until the source PR
// merges (Resolved=true is set by the GitHub poller on PR-merge detection).
type ExternalDep struct {
	SourceWorkspaceID string `json:"source_workspace_id"`
	SourceIssueNumber int    `json:"source_issue_number"`
	SourcePRNumber    int    `json:"source_pr_number"`
	// Resolved is set true by the poller when the source PR is merged.
	// Cleared issues immediately become eligible for auto-pull.
	Resolved bool `json:"resolved,omitempty"`
}

// FanoutStatus is the lifecycle state of a FanoutProposal.
type FanoutStatus string

const (
	FanoutStatusPending   FanoutStatus = "pending"
	FanoutStatusApproved  FanoutStatus = "approved"
	FanoutStatusDismissed FanoutStatus = "dismissed"
)

// FanoutProposal is a proposed follow-on issue for a sibling workspace,
// generated by the conductor-fanout skill after analyzing a PR diff.
type FanoutProposal struct {
	ID                string       `json:"id"`
	SourceWorkspaceID string       `json:"source_workspace_id"`
	SourceIssueNumber int          `json:"source_issue_number"`
	SourcePRNumber    int          `json:"source_pr_number"`
	TargetWorkspaceID string       `json:"target_workspace_id"`
	Title             string       `json:"title"`
	Body              string       `json:"body"`
	Labels            []string     `json:"labels,omitempty"`
	Status            FanoutStatus `json:"status"`
	// FiledIssueNumber is set when Status==approved and the issue was created.
	FiledIssueNumber *int      `json:"filed_issue_number,omitempty"`
	FiledIssueURL    string    `json:"filed_issue_url,omitempty"`
	CreatedAt        time.Time `json:"created_at"`
}

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
