# PrismConductor — Build Plan

**A goal-driven, multi-workspace agent orchestration desktop app.**

This document is the single source of truth for building PrismConductor. Hand it to an agent verbatim. Every section is intentionally specific. When in doubt, follow the schemas exactly — they are contracts, not suggestions.

---

## 1. What We're Building

A native desktop app that turns GitHub Issues into a Trello-style board, where:

- Multiple repos ("workspaces") feed into one unified board
- The user defines **Goals** that scope which issues are in play
- A lightweight **orchestrator agent** (local LLM) ranks the backlog by dependency-awareness given the active goal
- Worker agents (Claude Code) auto-spawn to **plan** issues without human intervention
- The user reviews plans, answers structured questions, approves, and the worker auto-resumes in **execute** mode
- The user is only on the hook for plan review and blocker resolution — never for launching, monitoring, or shepherding

The UI aesthetic is **Trello / Multica simple**: clean columns, minimal chrome, drag-and-drop, no overwhelming sidebars.

---

## 2. Non-Goals

- Not an issue tracker. GitHub Issues stays canonical.
- Not a code editor. No file tree, no diff view, no terminal-as-IDE.
- Not a chat interface. The user does not converse with the orchestrator.
- Not a replacement for `gh` CLI. Power users keep using it; the conductor is additive.
- Not multi-user or cloud. Single-user local desktop app. No accounts, no server.

---

## 3. Onboarding Requirements

A repo can be onboarded as a workspace if and only if these four checks pass. **Nothing else inside the repo is required** — no `CLAUDE.md`, no `.claude/`, no skills, no rules.

| Requirement | Why | How checked |
|---|---|---|
| Local path is a git repo | Need a working directory to spawn the agent | `git rev-parse --git-dir` |
| Has a GitHub remote | Issues are the work unit | `git remote get-url <name>` |
| `gh` CLI authenticated for the repo's owner | Required for issue read/write | `gh auth status` + `gh repo view` |
| `claude` CLI is on PATH | Worker runtime | `which claude` |

Everything beyond that floor is **optional enrichment** that improves plan quality but is not required for onboarding. See §3.1 for the maturation model and §15 for the bundled-skills system that makes bare-repo onboarding possible.

### 3.1 Skill Maturation Model

A workspace's quality of agent output improves as it accumulates conventions, but nothing is gated on the user authoring those conventions:

```
   Day 0                Day 30               Day 90              Day 180
   ┌──────┐             ┌──────┐             ┌──────┐            ┌──────┐
   │ Bare │─────────────│CLAUDE│─────────────│Rules │────────────│ Full │
   │ repo │             │ .md  │             │ tree │            │ suite│
   └──────┘             └──────┘             └──────┘            └──────┘
   Bundled              Bundled              Hybrid              Native
   only                 + rules              + start-issue       (PrismEngine
                                                                  pattern)
```

| Repo state | Conductor mode | Plan quality |
|---|---|---|
| Bare git repo, GitHub remote | Bundled | Basic — code-aware plans, no project rules |
| Adds `CLAUDE.md` | Bundled | Better — CLAUDE.md propagates to agent automatically |
| Adds `.claude/rules/` | Bundled | Path-targeted rules apply |
| Authors `/start-issue` skill | Hybrid | Repo-specific planning logic, conductor wraps it |
| Authors full skill suite | Native | Full repo-specific control (PrismEngine case) |

Phase 7 (Skill Studio, §13.7) provides the tooling that helps a repo graduate from one tier to the next without hand-authoring every artifact.

---

## 4. Stack

| Layer | Choice | Why |
|---|---|---|
| **Desktop shell** | Wails v2 | Native binary, web frontend, Go backend, single-file distribution |
| **Frontend** | React + TypeScript + Vite | Standard, well-supported by Wails templates |
| **UI library** | Tailwind CSS + shadcn/ui | Clean primitives, no heavy framework lock-in |
| **Drag-and-drop** | dnd-kit | Modern, accessible, keyboard-friendly |
| **Backend** | Go 1.22+ | Process management, GitHub API, SQLite, Ollama HTTP |
| **Local DB** | SQLite via `modernc.org/sqlite` (CGo-free) | Zero deps, embeddable |
| **PTY** | `github.com/creack/pty` | Standard for spawning interactive subprocesses |
| **GitHub API** | `github.com/google/go-github/v62` | Official client |
| **Orchestrator LLM** | Ollama → Qwen 2.5 14B Instruct | Local, free, structured output reliable |
| **Worker agent** | `claude` CLI (Claude Code) | Already installed on dev machines |

---

## 5. Architecture

```
┌──────────────────────────────────────────────────────────────────┐
│  PrismConductor (Wails)                                          │
│                                                                  │
│  ┌────────────────────────────────────────────────────────────┐  │
│  │  Frontend (React)                                          │  │
│  │  - Workspace switcher (top bar)                            │  │
│  │  - Goal pane (Active / Up Next / Achieved)                 │  │
│  │  - Board (TODO / PLAN / IN_PROGRESS / REVIEW / DONE)       │  │
│  │  - Plan review modal (markdown + question form)            │  │
│  │  - Session viewer (PTY stream)                             │  │
│  │  - Settings (workspaces, agent count, Ollama URL)          │  │
│  └────────────────────────────────────────────────────────────┘  │
│                              │                                   │
│                              │ Wails events / method calls       │
│                              ▼                                   │
│  ┌────────────────────────────────────────────────────────────┐  │
│  │  Go Backend                                                │  │
│  │                                                            │  │
│  │  ┌──────────────┐  ┌──────────────┐  ┌──────────────────┐  │  │
│  │  │ GitHubSvc    │  │ EventBus     │  │ Orchestrator     │  │  │
│  │  │ - fetch      │──│ - dispatches │──│ - rank backlog   │  │  │
│  │  │ - cache      │  │   to handlers│  │ - detect deps    │  │  │
│  │  │ - poll 5min  │  │              │  │ - move cards     │  │  │
│  │  └──────────────┘  └──────────────┘  └──────────────────┘  │  │
│  │                                                            │  │
│  │  ┌──────────────┐  ┌──────────────┐  ┌──────────────────┐  │  │
│  │  │ SessionMgr   │  │ WorkerPool   │  │ OllamaClient     │  │  │
│  │  │ - spawn PTY  │  │ - capacity   │  │ - structured JSON│  │  │
│  │  │ - tail/parse │  │ - pull queue │  │   prompts        │  │  │
│  │  └──────────────┘  └──────────────┘  └──────────────────┘  │  │
│  │                                                            │  │
│  │  ┌──────────────┐  ┌──────────────┐                        │  │
│  │  │ Store        │  │ NotifySvc    │                        │  │
│  │  │ (SQLite)     │  │ (OS native)  │                        │  │
│  │  └──────────────┘  └──────────────┘                        │  │
│  └────────────────────────────────────────────────────────────┘  │
└──────────────────────────────────────────────────────────────────┘
                              │
        ┌─────────────────────┼─────────────────────┐
        ▼                     ▼                     ▼
   GitHub API          claude CLI (PTY)        Ollama (HTTP)
   (per workspace)     (one per session)       localhost:11434
```

---

## 6. Core Data Model

### 6.1 Workspace

```go
type Workspace struct {
    ID            string            `json:"id"`            // "prismengine"
    DisplayName   string            `json:"display_name"`  // "PrismEngine"
    RepoPath      string            `json:"repo_path"`     // absolute filesystem path
    GitHubOwner   string            `json:"github_owner"`
    GitHubRepo    string            `json:"github_repo"`
    DefaultBranch string            `json:"default_branch"`
    Color         string            `json:"color"`         // hex, for card badge
    AgentEnv      EnvSpec           `json:"agent_env"`
    SkillProfile  SkillProfile      `json:"skill_profile"` // see 6.1.1
    Conventions   ConventionHints   `json:"conventions"`   // see 6.1.2
    Enabled       bool              `json:"enabled"`
}

type EnvSpec struct {
    EnvVars      map[string]string `json:"env_vars"`      // GAME_MODULE=gsx, etc.
    PreCommands  []string          `json:"pre_commands"`  // e.g. "source .venv/bin/activate"
    Shell        string            `json:"shell"`         // "/bin/bash" default
}
```

Stored in `~/Library/Application Support/PrismConductor/workspaces.json` (macOS) or platform equivalent.

#### 6.1.1 SkillProfile

Declares which skills this workspace uses for plan/execute/close. **Defaults to `bundled` mode** for new workspaces — no repo-side files required. See §15.7 for the bundled skill set.

```go
type SkillProfile struct {
    Mode                 SkillMode `json:"mode"`                  // bundled | hybrid | native
    UseConductorPlan     bool      `json:"use_conductor_plan"`
    UseConductorExecute  bool      `json:"use_conductor_execute"`
    UseConductorClose    bool      `json:"use_conductor_close"`
    NativePlanCommand    string    `json:"native_plan_command"`    // e.g. "/start-issue"
    NativeExecuteCommand string    `json:"native_execute_command"` // e.g. "/start-issue --resume-from-approved-plan"
    NativeCloseCommand   string    `json:"native_close_command"`   // e.g. "/check-and-close"
    ExtraContextFiles    []string  `json:"extra_context_files"`    // optional repo-local files to preload
}

type SkillMode string
const (
    SkillModeBundled SkillMode = "bundled"  // use conductor's universal skills only (default)
    SkillModeHybrid  SkillMode = "hybrid"   // mix native + conductor (repo has /start-issue but not /check-and-close, etc.)
    SkillModeNative  SkillMode = "native"   // use only repo's own skills (PrismEngine pattern)
)
```

Auto-detected on workspace add by scanning `<repo>/.claude/skills/` for known skill names. User can override in Settings.

#### 6.1.2 ConventionHints

Repo-specific build/test/lint commands the bundled `conductor-execute` skill uses. PrismEngine's native skills already know these; bundled mode learns them from this struct. Auto-sniffed on workspace add (see §15.10).

```go
type ConventionHints struct {
    TestCommand    string `json:"test_command"`    // e.g. ".venv/bin/python run_tests.py --type unit"
    BuildCommand   string `json:"build_command"`   // e.g. "go build ./..."
    LintCommand    string `json:"lint_command"`    // e.g. "ruff check"
    PyEnvPath      string `json:"py_env_path"`     // e.g. ".venv/bin/python"
    PackageManager string `json:"package_manager"` // "pnpm" | "npm" | "yarn" | "go" | "pip" | ""
}
```

### 6.2 Goal

```go
type Goal struct {
    ID            string       `json:"id"`           // uuid
    WorkspaceID   string       `json:"workspace_id"` // optional; null = cross-workspace
    Title         string       `json:"title"`        // "Get 2 types of each spell working"
    Intent        string       `json:"intent"`       // markdown, free-form description
    AcceptanceRule string      `json:"acceptance_rule"` // human-readable success condition
    IssueFilter   IssueQuery   `json:"issue_filter"`
    Status        GoalStatus   `json:"status"`       // backlog | active | achieved | abandoned
    Order         int          `json:"order"`        // position in backlog or history
    CreatedAt     time.Time    `json:"created_at"`
    AchievedAt    *time.Time   `json:"achieved_at,omitempty"`
    Notes         string       `json:"notes"`        // post-mortem
}

type IssueQuery struct {
    Labels    []string `json:"labels"`     // ["combat", "spell"]
    Milestone string   `json:"milestone"`  // optional
    FreeText  string   `json:"free_text"`  // optional title/body match
    Includes  []int    `json:"includes"`   // explicit additions
    Excludes  []int    `json:"excludes"`   // explicit removals
}

type GoalStatus string
const (
    GoalBacklog   GoalStatus = "backlog"
    GoalActive    GoalStatus = "active"
    GoalAchieved  GoalStatus = "achieved"
    GoalAbandoned GoalStatus = "abandoned"
)
```

**Invariant: at most one goal has status `active` at a time.** Enforced in `Store.SetGoalActive()`.

### 6.3 Issue (Local Mirror)

GitHub is canonical. We mirror only what we need locally and refresh on poll.

```go
type Issue struct {
    // GitHub-derived (read-only mirror):
    Number       int       `json:"number"`
    WorkspaceID  string    `json:"workspace_id"`
    Title        string    `json:"title"`
    Body         string    `json:"body"`
    Labels       []string  `json:"labels"`
    State        string    `json:"state"`        // open | closed
    URL          string    `json:"url"`
    UpdatedAt    time.Time `json:"updated_at"`

    // Conductor-managed:
    GoalID         *string         `json:"goal_id,omitempty"`
    Priority       float64         `json:"priority"`        // 0.0-1.0, orchestrator-assigned
    Dependencies   []int           `json:"dependencies"`    // issue numbers this depends on
    DepRationale   string          `json:"dep_rationale"`   // why these deps
    Column         BoardColumn     `json:"column"`          // local workflow state
    Plan           *Plan           `json:"plan,omitempty"`
    SessionID      *string         `json:"session_id,omitempty"`
    LastError      string          `json:"last_error,omitempty"`
}

type BoardColumn string
const (
    ColTodo       BoardColumn = "todo"
    ColPlan       BoardColumn = "plan"
    ColInProgress BoardColumn = "in_progress"
    ColReview     BoardColumn = "review"
    ColDone       BoardColumn = "done"
)
```

### 6.4 Plan

```go
type Plan struct {
    IssueNumber          int          `json:"issue_number"`
    WorkspaceID          string       `json:"workspace_id"`
    Revision             int          `json:"revision"`              // 1, 2, 3...
    PlanMarkdown         string       `json:"plan_markdown"`         // for human display
    FilesToModify        []FileIntent `json:"files_to_modify"`
    DependenciesDetected []int        `json:"dependencies_detected"`
    Questions            []Question   `json:"questions"`
    EstimatedComplexity  string       `json:"estimated_complexity"`  // small | medium | large
    ReadyToExecute       bool         `json:"ready_to_execute"`      // false until all questions answered
    GeneratedAt          time.Time    `json:"generated_at"`
    ApprovedAt           *time.Time   `json:"approved_at,omitempty"`
}

type FileIntent struct {
    Path   string `json:"path"`
    Intent string `json:"intent"` // "add" | "modify" | "delete" | "read-only"
}

type Question struct {
    ID       string       `json:"id"`
    Type     QuestionType `json:"type"`
    Prompt   string       `json:"prompt"`
    Options  []string     `json:"options,omitempty"`  // for choice types
    Default  *string      `json:"default,omitempty"`
    Required bool         `json:"required"`
    Answer   *string      `json:"answer,omitempty"`   // populated by user
}

type QuestionType string
const (
    QuestionSingleChoice QuestionType = "single_choice"
    QuestionMultiChoice  QuestionType = "multi_choice"
    QuestionFreeText     QuestionType = "free_text"
    QuestionYesNo        QuestionType = "yes_no"
)
```

### 6.5 Session

```go
type Session struct {
    ID          string      `json:"id"`           // uuid
    WorkspaceID string      `json:"workspace_id"`
    IssueNumber int         `json:"issue_number"`
    Mode        SessionMode `json:"mode"`         // plan | execute
    State       SessionState `json:"state"`       // running | waiting_for_input | blocked | completed | failed
    StartedAt   time.Time   `json:"started_at"`
    EndedAt     *time.Time  `json:"ended_at,omitempty"`
    PID         int         `json:"pid"`
    Transcript  string      `json:"-"`            // streamed to file, not loaded by default
    LastPrompt  string      `json:"last_prompt"`  // most recent stdin prompt detected
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
```

---

## 7. Event Bus

The orchestrator runs **only** when an event fires. No polling loops inside the orchestrator. Polling is contained to GitHub fetch (every 5 min) which itself emits events on diffs.

```go
type Event struct {
    Type      EventType
    Timestamp time.Time
    Payload   any
}

type EventType string
const (
    EvtGoalActivated     EventType = "goal_activated"
    EvtGoalUpdated       EventType = "goal_updated"
    EvtIssueAdded        EventType = "issue_added"
    EvtIssueClosed       EventType = "issue_closed"
    EvtIssueLabelChanged EventType = "issue_label_changed"
    EvtPlanReady         EventType = "plan_ready"
    EvtPlanApproved      EventType = "plan_approved"
    EvtPlanRejected      EventType = "plan_rejected"
    EvtPlanRevised       EventType = "plan_revised"
    EvtWorkerSlotFreed   EventType = "worker_slot_freed"
    EvtWorkerBlocked     EventType = "worker_blocked"
    EvtCardMovedManually EventType = "card_moved_manually"
    EvtAgentCountChanged EventType = "agent_count_changed"
)
```

`EventBus` is a simple in-process pub/sub. The orchestrator subscribes to all events and decides per-event whether action is needed (most events trigger a small reordering or pull; some are no-ops if the active goal isn't relevant).

---

## 8. Orchestrator Behavior (Per Event)

| Event | Orchestrator Action |
|---|---|
| `goal_activated` | Reload candidate issues. Run **rank+deps** prompt. Reorder TODO. |
| `goal_updated` (filter changed) | Reload candidate issues. Re-rank. |
| `issue_added` (new GH issue matching filter) | Run rank+deps for the single new issue + neighbors. Insert into TODO at correct position. |
| `issue_closed` | Remove from board (move to DONE column visually). Recompute unblocked status of dependents. Reorder TODO. |
| `issue_label_changed` | Re-evaluate goal membership. Add/remove from candidate set if needed. |
| `plan_ready` | Mark card "Plan Ready (rev N, K questions)". Notify user. **Do not auto-pull next** — wait for user approval first. |
| `plan_approved` | Move card to IN_PROGRESS. Resume worker session in execute mode. |
| `plan_rejected` | Move card back to TODO. Free worker slot. |
| `plan_revised` | Mark card "Plan Ready (rev N+1)". Notify user. |
| `worker_slot_freed` | Look at TODO top. If unblocked, pull into PLAN, spawn plan-mode worker. Else scan down for next unblocked. |
| `worker_blocked` | Mark card red. Notify user. Slot stays held. |
| `card_moved_manually` | Respect override. Do not re-rank. Log the manual move. |
| `agent_count_changed` (increased) | Pull additional issues into PLAN up to new capacity. |
| `agent_count_changed` (decreased) | Do not kill running plan workers. Wait for natural completion. |

---

## 9. The Two Critical Schemas

These are wire contracts between worker agents, orchestrator, and UI. **Do not deviate.**

### 9.1 Plan Mode Output Contract

When a worker finishes plan mode, it MUST emit a JSON file at `<repo>/.prismconductor/plans/<issue_number>-rev<N>.json` with this shape:

```json
{
  "issue_number": 1130,
  "workspace_id": "prismengine",
  "revision": 1,
  "plan_markdown": "## What I'll do\n\n1. Add new spell schema...\n2. Wire up...\n",
  "files_to_modify": [
    {"path": "games/gsx/scripts/spells/new_spell.py", "intent": "add"},
    {"path": "games/gsx/services/spell_service.py", "intent": "modify"},
    {"path": "tests/integration/gsx/spells/test_new_spell.py", "intent": "add"}
  ],
  "dependencies_detected": [1116, 1117],
  "questions": [
    {
      "id": "q1",
      "type": "single_choice",
      "prompt": "Should the new spell use FIRE_LORE seed=12 (standard) or seed=14 (gentle utility)?",
      "options": ["seed=12", "seed=14", "seed=10 (steep)"],
      "required": true
    }
  ],
  "estimated_complexity": "medium",
  "ready_to_execute": false
}
```

`ready_to_execute` is `true` only when `questions` is empty (or all answered in a revision).

### 9.2 AskUserQuestion Schema (UI-rendered)

The UI renders each `Question` as a form field:
- `single_choice` → radio group
- `multi_choice` → checkbox group
- `free_text` → textarea
- `yes_no` → toggle

On submit, the UI:
1. Writes answers back to a sibling file `<repo>/.prismconductor/answers/<issue_number>-rev<N>.json`
2. Sends a signal to the worker session via PTY (the worker reads this file when prompted)
3. Worker re-runs phases 3-4 of `/start-issue`, emits `<issue_number>-rev<N+1>.json`
4. UI reloads card

---

## 10. Worker Spawning

### 10.1 Plan Mode Spawn

```go
func (s *SessionManager) SpawnPlan(ws Workspace, issue Issue) (*Session, error) {
    promptArgs := []string{
        "/start-issue", strconv.Itoa(issue.Number),
        "--emit-plan-json",  // new flag we add to /start-issue skill
    }
    cmd := exec.Command("claude", promptArgs...)
    cmd.Dir = ws.RepoPath
    cmd.Env = append(os.Environ(), envSpecToSlice(ws.AgentEnv)...)

    f, err := pty.Start(cmd)
    if err != nil { return nil, err }

    sess := &Session{
        ID:          uuid.NewString(),
        WorkspaceID: ws.ID,
        IssueNumber: issue.Number,
        Mode:        ModePlan,
        State:       StateRunning,
        StartedAt:   time.Now(),
        PID:         cmd.Process.Pid,
    }

    go s.tailAndParse(sess, f)
    return sess, nil
}
```

### 10.2 Execute Mode Spawn

```go
func (s *SessionManager) SpawnExecute(ws Workspace, issue Issue, plan Plan) (*Session, error) {
    // Plan + answered questions are persisted in the repo's .prismconductor/ dir.
    // Worker reads them via the resume-from-plan flow.
    promptArgs := []string{
        "/start-issue", strconv.Itoa(issue.Number),
        "--resume-from-approved-plan", strconv.Itoa(plan.Revision),
    }
    // ... same spawn logic as Plan, but Mode = ModeExecute
}
```

### 10.3 PTY Output Parsing

The session goroutine watches PTY output for these patterns:

| Pattern | Action |
|---|---|
| `"Question: "` followed by structured prompt | State → `waiting_for_input`, emit notification |
| `"Plan written to .prismconductor/plans/"` | Read JSON, populate `Issue.Plan`, emit `EvtPlanReady` |
| `"Work complete."` | State → `completed`, emit `EvtWorkerSlotFreed` |
| `"BLOCKED:"` line | State → `blocked`, emit `EvtWorkerBlocked` |
| Process exit non-zero | State → `failed`, emit `EvtWorkerSlotFreed` |

These patterns must be defined as constants in `session_patterns.go` and matched via simple string contains, not regex.

### 10.4 Skill Mode Dispatch

The example in 10.1 hardcodes `/start-issue` — the real implementation dispatches based on the workspace's `SkillProfile.Mode` (see §6.1.1).

```go
func (s *SessionManager) buildPlanCommand(ws Workspace, issue Issue) []string {
    switch ws.SkillProfile.Mode {
    case SkillModeNative:
        // Repo's own skill (PrismEngine pattern). Requires native skill to support --emit-plan-json.
        return []string{
            "claude",
            ws.SkillProfile.NativePlanCommand,
            strconv.Itoa(issue.Number),
            "--emit-plan-json",
        }
    case SkillModeHybrid:
        // Repo has /start-issue but no JSON-emit support. Bundled wrapper invokes native + post-processes.
        return []string{
            "claude",
            "/conductor-plan",
            "--native-cmd", ws.SkillProfile.NativePlanCommand,
            "--issue", strconv.Itoa(issue.Number),
        }
    case SkillModeBundled:
        // No native skill. Universal bundled planner.
        return []string{
            "claude",
            "/conductor-plan",
            "--issue", strconv.Itoa(issue.Number),
            "--repo", ws.RepoPath,
        }
    }
    return nil
}
```

Bundled mode passes `CLAUDE_SKILLS_PATH=~/.prismconductor/skills/` (or the platform equivalent) so the spawned Claude Code session sees the universal skills. The repo's own `CLAUDE.md` and `.claude/rules/` (if any) are loaded automatically by Claude Code from `cwd` — bundled skills do not duplicate or override them.

`buildExecuteCommand` and `buildCloseCommand` follow the same dispatch pattern with `NativeExecuteCommand` and `NativeCloseCommand` respectively.

---

## 11. Local LLM (Orchestrator) Integration

### 11.1 Ollama Setup

User must have Ollama installed and running. App detects via `GET http://localhost:11434/api/tags`. If model not present, app prompts user to run `ollama pull qwen2.5:14b-instruct`.

Configurable in Settings:
- Ollama URL (default `http://localhost:11434`)
- Model name (default `qwen2.5:14b-instruct`)
- Temperature (default `0.0` for determinism)

### 11.2 Rank + Dependency Prompt

System prompt:
```
You are a backlog organizer. Given a goal and a list of GitHub issues, you produce
a JSON object describing dependency relationships and a priority ordering.

Rules:
- A "primitive" is an issue that other issues depend on (foundational work).
- A "dependent" is an issue that needs primitives done first.
- Use issue body text mentioning "depends on", "requires", "blocked by", or
  numeric issue references (#NNNN) as hard signals.
- Use semantic understanding to infer dependencies when not explicit.
- Output valid JSON only. No prose. No markdown fences.
```

User prompt template:
```
GOAL:
Title: {{.Goal.Title}}
Intent: {{.Goal.Intent}}
Acceptance: {{.Goal.AcceptanceRule}}

ISSUES (id, title, body excerpt):
{{range .Issues}}
#{{.Number}} {{.Title}}
{{.BodyExcerpt}}
---
{{end}}

Return JSON:
{
  "ordering": [<issue_number>, ...],  // top-to-bottom priority order
  "dependencies": [
    {"issue": <num>, "depends_on": [<num>, ...], "rationale": "..."}
  ],
  "primitives": [<issue_number>, ...],
  "rationale": "one paragraph summary"
}
```

`Issues[].BodyExcerpt` is truncated to 800 chars to keep prompts under ~8k tokens for a 50-issue backlog.

### 11.3 Caching

Each issue's dependency analysis is cached by `(issue_number, body_hash)`. Re-analyze only when body changes or when goal changes.

---

## 12. Frontend UI Spec

### 12.1 Layout

```
┌───────────────────────────────────────────────────────────────────┐
│ PrismConductor                              [Settings] [Account] │
├───────────────────────────────────────────────────────────────────┤
│ Workspace: [All ▼] [prismengine] [prismeditor] [pe_ai_agents]    │
├───────────────────────────────────────────────────────────────────┤
│ Active Goal: 🎯 Get 2 types of each spell working                 │
│ ─────────────────────────────────────────────────────────────     │
│ Up Next: ▸ Combat polish    ▸ Admin parity                        │
│ Past:    ▸ Magic foundations (achieved 2026-04-15)                │
├───────────────────────────────────────────────────────────────────┤
│                                                                   │
│  TODO (12)        PLAN (2)         IN_PROGRESS (1)   REVIEW (1)   │
│  ┌────────┐      ┌────────┐       ┌────────┐        ┌────────┐    │
│  │ #1116  │      │ #1130  │       │ #1145  │        │ #1099  │    │
│  │ pe-eng │      │ pe-eng │       │ editor │        │ pe-eng │    │
│  │ 🔴 prim│      │ ⏸ Plan │       │ ▶ work │        │ ✓ PR   │    │
│  │ Pri 1  │      │ Ready  │       │ ing    │        │ #823   │    │
│  └────────┘      │ (3 Q)  │       └────────┘        └────────┘    │
│                  └────────┘                                       │
│  ┌────────┐                                                       │
│  │ #1117  │                                                       │
│  │ pe-eng │                                                       │
│  │ 🔴 prim│                                                       │
│  └────────┘                                                       │
│                                                                   │
│  ┌────────┐                                                       │
│  │ #1131  │                                                       │
│  │ pe-eng │                                                       │
│  │ blocked│                                                       │
│  │ by 1116│                                                       │
│  └────────┘                                                       │
│                                                                   │
├───────────────────────────────────────────────────────────────────┤
│ Worker Pool: 2/3 active     [+ Add slot] [- Remove slot]          │
└───────────────────────────────────────────────────────────────────┘
```

### 12.2 Card Anatomy

Each card shows:
- Issue number + workspace badge (color-coded by workspace)
- Truncated title (one line)
- State icon: `🔴 primitive` / `⏸ Plan Ready` / `▶ working` / `✓ PR open` / `🚫 blocked by #X`
- Priority indicator (small, top-right)

Click → opens detail pane (right drawer or modal).

### 12.3 Plan Review Modal

Triggered when card in PLAN column is clicked.

```
┌─────────────────────────────────────────────────────────────┐
│ Plan: #1130 — GSX magic crit table refactor    [×]          │
│ ───────────────────────────────────────────────────────────  │
│ Workspace: prismengine    Revision: 1    Complexity: medium │
│ Generated: 2 minutes ago                                     │
│                                                              │
│ ## What the agent plans to do                                │
│ [rendered markdown]                                          │
│                                                              │
│ ## Files to modify                                           │
│ + games/gsx/scripts/spells/new_spell.py                      │
│ ~ games/gsx/services/spell_service.py                        │
│ + tests/integration/gsx/spells/test_new_spell.py             │
│                                                              │
│ ## Detected dependencies                                     │
│ • Depends on #1116 (open) ⚠                                  │
│                                                              │
│ ❓ Questions (3)                                              │
│ ┌──────────────────────────────────────────────────────┐     │
│ │ 1. Should the new spell use seed=12 or seed=14?      │     │
│ │ ( ) seed=12                                          │     │
│ │ ( ) seed=14                                          │     │
│ │ ( ) seed=10 (steep)                                  │     │
│ └──────────────────────────────────────────────────────┘     │
│ [... more questions ...]                                     │
│                                                              │
│ [ Submit answers & request revised plan ]                    │
│ [ Approve as-is ]   [ Reject ]                               │
└─────────────────────────────────────────────────────────────┘
```

### 12.4 Session Viewer

Click on IN_PROGRESS card → drawer with live PTY tail (read-only). User can scroll back through transcript. Buttons: `Pause`, `Kill`, `Send input` (free-text injection if needed for ad-hoc unblocking).

### 12.5 Settings Panel

- Workspaces (CRUD)
- Worker pool capacity (1-5)
- Ollama URL / model
- GitHub token (uses `gh auth status` token by default; manual override available)
- Notification preferences (mute / sound / banner)
- Polling interval (default 5 min, configurable 1-30)

---

## 13. Persistence

SQLite database at `~/Library/Application Support/PrismConductor/conductor.db`.

Tables:
- `workspaces` — registry
- `goals` — all goals with status
- `issues` — local mirror with conductor-managed fields
- `plans` — historical plan revisions, JSON blob
- `sessions` — session log, transcript path
- `events` — append-only event log for debugging
- `settings` — key-value store for config

Transcript files live alongside DB at `transcripts/<session_id>.log`. Plans + answers live in each repo at `<repo>/.prismconductor/` (git-ignored, but visible to the worker on disk).

---

## 14. Build Phases

Each phase is independently shippable. Phase 1 alone is useful day-one.

### Phase 1: Conductor MVP (7 days)
**Goal: drag an issue to PLAN, agent runs, get notified when it needs you.**

- Day 1: Wails skeleton, Tailwind + shadcn/ui setup, settings page stub, SQLite init
- Day 2: Workspace registry CRUD (add/edit/delete via JSON config + UI)
- Day 3: GitHub fetch fan-out (parallel per workspace, cache to SQLite, 5min poll)
- Day 4: Board UI (5 columns, drag-and-drop with dnd-kit, workspace filter)
- Day 5: Session spawning (PTY via creack/pty, cwd + env from workspace)
- Day 6: PTY tail + parse, basic state detection (running/waiting/done)
- Day 7: OS notifications, session viewer drawer, kill button

Checkpoint: User can drag #1130 to PLAN, agent runs `/start-issue 1130`, user sees session output, gets notification on input prompt.

### Phase 2: Goals + Event Bus (3 days)
**Goal: scope the board to an active goal, emit events on board changes.**

- Day 8: Goal data model + CRUD UI (active / up next / achieved)
- Day 9: Goal filter applied to TODO column (only matching issues visible)
- Day 10: EventBus implementation, all UI actions and GitHub diffs emit events

Checkpoint: User defines a goal, board filters to matching issues, all state changes emit events to the bus (verified in event log table).

### Phase 3: Local LLM Orchestrator (4 days)
**Goal: backlog auto-orders by dependency, primitives float to top.**

- Day 11: Ollama client, model presence check, settings UI for endpoint
- Day 12: Rank+deps prompt, JSON parsing, schema validation
- Day 13: Orchestrator event handlers (goal_activated, issue_added, issue_closed)
- Day 14: TODO column reordering on rank result, dependency badges on cards

Checkpoint: Activate a goal → Ollama analyzes issues → TODO reorders with primitives at top → blocked issues show "blocked by #X" badge.

### Phase 4: Plan Mode + Q&A Loop (4 days)
**Goal: agents pre-plan issues, user reviews + answers questions, approval triggers execute.**

- Day 15: `/start-issue --emit-plan-json` flag in the skill (this is a separate edit to your `.claude/` skills, not the Wails app)
- Day 16: Plan JSON parsing, plan storage in SQLite, plan review modal UI
- Day 17: Question form rendering by type, answer write-back, revision loop trigger
- Day 18: Approve button → spawn execute-mode worker, resume from approved plan

Checkpoint: Card in PLAN shows "Plan Ready (3 questions)" → user clicks → modal renders plan + questions → user answers → "Revising..." → new revision → user approves → card moves to IN_PROGRESS, execute-mode worker spawns.

### Phase 4.5: Skill Bundle (2 days)
**Goal: bare repos work day-one without authoring any skills locally.**

- Day 18.5: Author and ship the four bundled skills (`conductor-plan`, `conductor-execute`, `conductor-close`, `conductor-question`). Distribute via the Wails binary, extracted on first run to `~/.prismconductor/skills/`.
- Day 18.6: Skill profile detection on workspace add (auto-fill `SkillProfile.Mode`). Skill mode dispatch in `SessionManager` (see §10.4).
- Day 18.7: Settings UI per workspace — `SkillProfileEditor.tsx` showing detected profile + override controls, plus a read-only viewer for the four bundled skills with "Open in editor" link for power users.

Checkpoint: Add a bare repo (no `CLAUDE.md`, no `.claude/`) → click Add Workspace → drag an issue to PLAN → bundled `conductor-plan` runs in that repo's cwd → emits a real plan JSON → reviewable in the same modal as native-mode plans.

### Phase 5: Auto-Pull Worker Pool (2 days)
**Goal: free slots automatically pull next unblocked issue into PLAN.**

- Day 19: WorkerPool capacity tracking, slot-freed event handling
- Day 20: Pull-from-TODO logic with blocked-skip, manual override respect

Checkpoint: Set agent count = 3 → top 3 unblocked issues auto-move to PLAN → as user approves and execute-mode workers run, new TODO items auto-pull in.

### Phase 6: Polish (2-3 days)
- Goal achievement detection + auto-advance to next goal
- Dependency graph visualizer (small SVG on goal pane)
- Session transcript search
- Per-workspace agent presets
- Distribution: signed `.dmg` and `.exe` builds via Wails

### Phase 7: Skill Studio (4-5 days, deferrable to v1.1)
**Goal: graduate bare repos toward maturity by harvesting patterns from accumulated session transcripts.**

- Day 23: **Convention sniffer** — Go-only static analysis (`internal/sniffer/`). Scans file extensions, naming patterns, test layout, package files, CI configs. Outputs `ConventionHints` deterministically without LLM. Runs on workspace add and on demand.
- Day 24: **Transcript pattern detector** (`internal/transcriptpattern/`) — scans the conductor's stored session logs to surface: (a) recurring corrections (3+ similar user→agent corrections), (b) recurring workflows (3+ similar multi-step sequences), (c) absence signals (e.g., no `CLAUDE.md` and agent kept guessing repo conventions).
- Day 25: **`/conductor-bootstrap-claudemd`** bundled skill. Inputs: repo path, recent correction transcripts, sniffer output. Output: first-draft `CLAUDE.md` at repo root with stack, commands, conventions, and a "Things the agent kept getting wrong" section. Single-shot. Opens a PR.
- Day 26: **`/conductor-extract-rule`** and **`/conductor-extract-skill`** bundled skills. Both consume specific transcript turns (selected by user OR surfaced by the pattern detector) and emit drafts in PrismEngine's rule/skill voice (frontmatter, **Why:**, **How to apply:**, anti-patterns, verification grep).
- Day 27: **Skill Studio UI panel** (`SkillStudio.tsx`) per workspace. Shows current skill profile, pattern-detector suggestions ("Extract rule: corrected 4 times"), manual launch buttons, list of existing artifacts.

Checkpoint: A bare repo accumulates 30 transcripts → Skill Studio surfaces 3 suggestions → user one-clicks "Bootstrap CLAUDE.md" → bundled skill drafts file → user reviews + merges PR → workspace auto-promotes from "no CLAUDE.md detected" without changing skill mode.

**Phase 7 is optional and deferrable.** Phases 1-6 are useful without it. Ship Phase 7 in v1.1 after Phases 1-6 prove the daily-driver value.

---

## 15. Critical Implementation Notes

### 15.1 The `/start-issue` Skill Must Be Updated

The conductor relies on `/start-issue` emitting structured JSON. This requires a small change to the existing skill in PrismEngine's `.claude/skills/start-issue.md`:

- Add `--emit-plan-json` flag handling
- Plan output goes to `<repo>/.prismconductor/plans/<num>-rev<N>.json`
- Skill stops at proposal gate (does not proceed to implementation)
- Add `--resume-from-approved-plan <N>` flag to skip discovery and proceed to implementation using the approved plan

This work is **out of scope for the conductor build itself** but is a prerequisite for Phase 4. Track as a separate PrismEngine issue.

### 15.2 GitHub Auth

Use the user's existing `gh auth token` output. Run `gh auth token` once at startup, cache the token, refresh on 401. Do not implement OAuth flows.

### 15.3 Process Lifecycle

If the conductor app exits while sessions are running:
- Sessions continue running (they're independent processes)
- On restart, app re-attaches to known session PIDs by reading `sessions` table
- If a PID is dead, mark session as `failed` and emit `worker_slot_freed`

### 15.4 Single Active Goal Constraint

Enforced at write time in `Store.SetGoalActive(id)`:
```sql
BEGIN;
UPDATE goals SET status = 'backlog' WHERE status = 'active';
UPDATE goals SET status = 'active' WHERE id = ?;
COMMIT;
```

### 15.5 Drag-and-Drop Reorder Authority

When user drags a card within the same column, that's a manual override of orchestrator's ordering. Persist the manual order with a `manual_order` column. Orchestrator-set `priority` becomes a tiebreaker only. Manual moves emit `EvtCardMovedManually` (logged, not acted on).

When user drags between columns, that's a workflow state change. Emit appropriate event.

### 15.6 Notification Hygiene

- One notification per state transition. No spam.
- Group notifications by workspace.
- Quiet hours configurable.
- App badge count = total cards in `waiting_for_input` or `blocked` state.

### 15.7 Bundled Skills

The conductor ships four universal skills with the binary:

| Skill | Purpose |
|---|---|
| `conductor-plan` | Universal `/start-issue` analogue. Reads issue, greps repo, reads any `CLAUDE.md` + `.claude/rules/` present, emits plan JSON, stops. No code mutation. |
| `conductor-execute` | Resumes from approved plan + answered questions. Reads repo conventions from `ConventionHints` for test/build commands. Implements per plan. Opens draft PR. |
| `conductor-close` | Universal `/check-and-close` analogue. Posts completion summary, closes issue. No commit/push beyond what was already done. |
| `conductor-question` | Helper invoked inline when a worker needs to emit a structured question mid-execution. Writes to `<repo>/.prismconductor/questions/<id>.json`. |

**Distribution**: skills are `embed.FS`-bundled in the Go binary and extracted to `~/.prismconductor/skills/` on first run. Power users can edit them after extraction; the conductor uses the on-disk copy, not the embedded one, after first run.

**Activation**: when spawning a worker session, the conductor sets `CLAUDE_SKILLS_PATH=~/.prismconductor/skills/` (or whichever env var Claude Code honors for additional skill paths). The repo's own `CLAUDE.md` and `.claude/rules/` are loaded automatically by Claude Code from `cwd`.

**Authoring stability**: bundled skills are hand-curated and stable. Do NOT use the Phase 7 skill-authoring skills to regenerate them. The recursion stops at one level (Phase 7 authors per-repo skills, not bundled ones).

### 15.8 Graceful Degradation for Bare Repos

The conductor handles missing repo enrichments silently — no warnings, no blocking gates:

| Scenario | Behavior |
|---|---|
| No `CLAUDE.md` | Bundled skill works without it. Don't warn. |
| No `.claude/rules/` | Bundled skill works without it. Don't warn. |
| No test runner detected | `conductor-execute` skips test step, notes "No tests run (no runner detected)" in PR description. Don't fail. |
| Empty backlog | Board shows empty TODO column. Goal definition still works. |
| GitHub Issues disabled on repo | Onboarding fails at `gh repo view` check with clear error: "Issues disabled on github.com/x/y — enable Issues in repo settings or use a different repo." |
| Default branch not `main` | Auto-detected from `gh repo view --json defaultBranchRef`. No user action. |
| Fork repo | Works fine. Issues live on the fork unless user reconfigures remote. |
| Private repo | `gh` handles auth. Conductor doesn't care. |

### 15.9 Skill Maturation Model (referenced from §3.1)

The bundled→hybrid→native progression is captured in §3.1 and not duplicated here. Implementation note: the dispatch logic in §10.4 is the only place mode affects worker spawning. All other code paths treat all three modes identically.

### 15.10 Convention Sniffer (Phase 7)

`internal/sniffer/` runs deterministic static analysis on workspace add and on demand:

- File extensions → primary languages (`.py` / `.go` / `.ts` / `.tsx` / `.rs`)
- File-naming patterns in `src/`, `lib/`, etc. → `snake_case` vs `camelCase` vs `kebab-case`
- Test file location heuristics (`tests/`, `__tests__/`, `*_test.go`, `*_test.py`)
- Package files: `package.json` → npm/pnpm/yarn (read `packageManager` field), `go.mod` → Go, `pyproject.toml` / `pytest.ini` / `requirements.txt` → Python
- Virtual env detection: `.venv/`, `venv/`, `env/`
- CI hints: `.github/workflows/` for test/lint/build commands
- Existing `.claude/` structure for skill profile auto-detection

Outputs `ConventionHints` plus a "skeleton suggestions" payload that `/conductor-bootstrap-claudemd` consumes. No LLM, fully reproducible, fast (<2s on a typical repo).

### 15.11 Transcript-Driven Skill Authoring (Phase 7)

The three Skill Studio skills (`bootstrap-claudemd`, `extract-rule`, `extract-skill`) all share a critical property: **they never generate from a blank page**. Each one consumes observed behavior from the conductor's transcript log:

| Skill | Input from transcripts |
|---|---|
| `bootstrap-claudemd` | All sessions on this workspace + all corrections the user made |
| `extract-rule` | A specific selected correction turn + 5 surrounding turns for context |
| `extract-skill` | 3+ similar multi-step sequences identified by pattern detector |

The `internal/transcriptpattern/` pattern detector classifies user→agent corrections by similarity (lightweight: shared n-grams over the corrected sentence). When 3+ similar corrections accumulate, it surfaces a Skill Studio suggestion to extract a rule. This keeps the skill-authoring grounded in what actually happened, not what an LLM imagines should happen.

**Quality expectation**: the first round of generated artifacts is mediocre. User reviews and edits before merge. Refinements over time become better training data for the next round. After ~6 months of use, a workspace's rule tree should approximate PrismEngine's current quality, with much less manual authoring.

---

## 16. Out of Scope (For Now)

- Multi-user / cloud sync
- Cross-repo refactor agents (workspace isolation is sacred)
- Agent-to-agent communication
- Custom skill authoring within conductor for v1.0 (Phase 7 Skill Studio adds this in v1.1)
- Anything beyond Claude Code as worker (no Codex / Cursor / etc. multi-runtime)
- Mobile companion app
- Web-only deployment
- Encrypted local storage (SQLite is plaintext; rely on OS disk encryption)

These are explicitly deferred. Resist scope creep.

---

## 17. Repo Layout (New Repo: `prismconductor`)

```
prismconductor/
├── README.md
├── CLAUDE.md                        # conductor-specific dev rules
├── go.mod
├── go.sum
├── wails.json
├── main.go                          # Wails entry
├── app.go                           # bound methods exposed to frontend
├── internal/
│   ├── github/                      # GitHub API client
│   │   ├── client.go
│   │   └── poll.go
│   ├── orchestrator/
│   │   ├── orchestrator.go
│   │   ├── prompts.go
│   │   └── handlers.go
│   ├── ollama/
│   │   └── client.go
│   ├── session/
│   │   ├── manager.go
│   │   ├── pty.go
│   │   └── patterns.go
│   ├── workerpool/
│   │   └── pool.go
│   ├── store/
│   │   ├── sqlite.go
│   │   ├── migrations/
│   │   └── queries.go
│   ├── eventbus/
│   │   └── bus.go
│   ├── notify/
│   │   └── notify.go
│   ├── workspace/
│   │   ├── registry.go
│   │   └── detect.go              # SkillProfile auto-detection (Phase 4.5)
│   ├── skills/
│   │   └── bundle/                # embed.FS source for bundled skills
│   │       ├── conductor-plan.md
│   │       ├── conductor-execute.md
│   │       ├── conductor-close.md
│   │       ├── conductor-question.md
│   │       └── bundle.go          # //go:embed FS, extract on first run
│   ├── sniffer/                   # Phase 7: deterministic convention detection
│   │   ├── sniffer.go
│   │   ├── languages.go
│   │   ├── tests.go
│   │   └── packagers.go
│   └── transcriptpattern/         # Phase 7: pattern detection over session logs
│       ├── classifier.go
│       └── suggester.go
├── frontend/
│   ├── package.json
│   ├── vite.config.ts
│   ├── tailwind.config.ts
│   ├── src/
│   │   ├── App.tsx
│   │   ├── components/
│   │   │   ├── Board.tsx
│   │   │   ├── Card.tsx
│   │   │   ├── Column.tsx
│   │   │   ├── PlanModal.tsx
│   │   │   ├── QuestionForm.tsx
│   │   │   ├── SessionDrawer.tsx
│   │   │   ├── GoalPane.tsx
│   │   │   ├── WorkspaceSwitcher.tsx
│   │   │   ├── Settings.tsx
│   │   │   ├── SkillProfileEditor.tsx   # Phase 4.5
│   │   │   └── SkillStudio.tsx          # Phase 7
│   │   ├── hooks/
│   │   ├── stores/                  # zustand or similar
│   │   └── lib/
│   └── ...
├── docs/
│   ├── architecture.md
│   ├── plan-schema.md
│   ├── orchestrator-prompts.md
│   └── bundled-skills.md            # Phase 4.5: docs for the four universal skills
└── scripts/
    └── build.sh
```

---

## 18. First-Day Deliverable Checklist

For the agent picking this up tomorrow:

- [ ] `wails init -n prismconductor -t react-ts` from `/Users/dino/Documents/git/`
- [ ] Add Tailwind, shadcn/ui, dnd-kit, zustand to frontend
- [ ] Add `creack/pty`, `google/go-github`, `modernc.org/sqlite` to Go modules
- [ ] Stub the Go service interfaces in `internal/` matching this doc's data model
- [ ] Stub frontend components matching the layout in §12
- [ ] Wire one end-to-end demo: button click → spawn `claude --version` via PTY → show output in drawer
- [ ] Commit, push to new GitHub repo `prismconductor`
- [ ] Open issue #1 "Phase 1: Conductor MVP — Day 2 work"

If end-of-day-one demo works, the rest of Phase 1 is mechanical.

---

## 19. Success Criteria

This project is successful when:

1. Owner can hand off issues to the conductor and walk away
2. Owner spends < 20% of "agent time" on launching/monitoring (vs current ~80%)
3. Active goal scoping reliably keeps focus — no more "what should I work on next?"
4. Plan review + Q&A loop replaces 3-4 manual prompt-engineering rounds with one structured exchange
5. Multi-workspace isolation never leaks: a prismeditor agent never sees PrismEngine's CLAUDE.md
6. Local LLM orchestrator runs reliably on Owner's hardware with no API costs
7. **A bare repo with no `.claude/` directory and no `CLAUDE.md` can be onboarded and used productively within 60 seconds of clicking Add Workspace** (bundled-mode floor)
8. After Phase 7 ships, a workspace can graduate from bundled → +CLAUDE.md → +rules → native via Skill Studio suggestions, with the user reviewing and approving each generated artifact

If Phase 1 alone hits criteria 1, 2, and 5, this is already a daily-driver win and Phases 2-6 are pure upside. Phase 4.5 is required to hit criterion 7. Phase 7 is required to hit criterion 8.

---

## 20. Open Questions for Owner

Before Phase 4 starts, resolve:

1. **Plan format granularity** — should plans include exact code snippets or stay at intent level? (Recommendation: intent level, no code in plans)
2. **Multi-question flow** — answer all at once or one-at-a-time wizard? (Recommendation: all at once, single submit)
3. **Goal achievement detection** — automatic (all child issues closed) or manual click? (Recommendation: manual, with "all done" suggestion banner)
4. **Worker preemption** — if a higher-priority issue arrives mid-plan, do we kill and restart? (Recommendation: never, finish what's started)
5. **Failure replay** — on session failure, auto-retry once? (Recommendation: no, surface to user, they decide)

Before Phase 4.5 starts, resolve:

6. **Bundled skill storage** — embedded in the binary only, or extracted to `~/.prismconductor/skills/` on first run for power-user editing? (Recommendation: extracted, so users can fork them; conductor uses the on-disk copy after first run)
7. **Skill mode override granularity** — per-workspace mode is global, or can a single workspace use bundled-plan + native-close? (Recommendation: per-skill flags already exist on `SkillProfile.UseConductor*` — yes, mix freely)

Before Phase 7 starts, resolve:

8. **Phase 7 ship target** — v1.0 or v1.1 follow-on? (Recommendation: v1.1, after Phases 1-6 prove the daily-driver value)
9. **Pattern detector aggressiveness** — surface suggestions after 3 similar corrections (sensitive) or 5 (conservative)? (Recommendation: 3, with user-configurable threshold in Settings)
10. **Skill Studio output target** — does the bootstrap skill open a PR or just write the file locally? (Recommendation: PR, so the user reviews via familiar GitHub UI before merging)

---

## 21. Glossary

- **Workspace** — a registered local repo with its own conventions (a `CLAUDE.md` + `.claude/rules/`)
- **Goal** — a user-defined scope that filters which issues are eligible for the board
- **Active goal** — the one goal currently driving board content (max 1)
- **Primitive** — an issue that other issues depend on
- **Dependent** — an issue blocked by one or more primitives
- **Plan mode** — worker runs `/start-issue` discovery only, stops at proposal
- **Execute mode** — worker resumes with approved plan and implements
- **Orchestrator** — local-LLM-backed organizer that ranks and routes issues on event triggers
- **Worker** — Claude Code session executing actual work
- **Slot** — one unit of worker pool capacity (user-set count)

---

**End of plan. Hand this to an agent. Build Phase 1 first. Ship it before touching Phase 2.**
