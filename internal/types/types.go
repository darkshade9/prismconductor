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
}

type EnvSpec struct {
	EnvVars     map[string]string `json:"env_vars"`
	PreCommands []string          `json:"pre_commands"`
	Shell       string            `json:"shell"`
}

type SkillProfile struct {
	Mode                 SkillMode `json:"mode"`
	UseConductorPlan     bool      `json:"use_conductor_plan"`
	UseConductorExecute  bool      `json:"use_conductor_execute"`
	UseConductorClose    bool      `json:"use_conductor_close"`
	NativePlanCommand    string    `json:"native_plan_command"`
	NativeExecuteCommand string    `json:"native_execute_command"`
	NativeCloseCommand   string    `json:"native_close_command"`
	ExtraContextFiles    []string  `json:"extra_context_files"`
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
	ID            string       `json:"id"`
	WorkspaceID   string       `json:"workspace_id"`
	IssueNumber   int          `json:"issue_number"`
	Mode          SessionMode  `json:"mode"`
	State         SessionState `json:"state"`
	StartedAt     time.Time    `json:"started_at"`
	EndedAt       *time.Time   `json:"ended_at,omitempty"`
	PID           int          `json:"pid"`
	Transcript    string       `json:"-"`
	LastPrompt    string       `json:"last_prompt"`
	BlockedReason string       `json:"blocked_reason,omitempty"`
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
}

type SessionMode string

const (
	ModePlan    SessionMode = "plan"
	ModeExecute SessionMode = "execute"
)

type SessionState string

const (
	StateRunning         SessionState = "running"
	StateWaitingForInput SessionState = "waiting_for_input"
	StateBlocked         SessionState = "blocked"
	StateCompleted       SessionState = "completed"
	StateFailed          SessionState = "failed"
)
