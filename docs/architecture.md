# Architecture

This document describes the current PrismConductor architecture. The historical launch plan lives in `PRISMCONDUCTOR_PLAN.md`, but current behavior is documented here.

## System Shape

PrismConductor is a local Wails desktop app with a Go backend and React frontend.

```text
Frontend (React/Wails)
  Board, cards, plan modal, settings, diagnostics
        |
        v
Go backend
  workspace registry
  SQLite store
  GitHub poller
  event bus
  orchestrator
  worker pool registry
  session manager
        |
        +--> subprocess provider CLIs
        +--> in-process harness providers
        +--> local git worktrees
        +--> GitHub API / gh CLI
```

GitHub issues and PRs remain the external source of truth. PrismConductor stores local state for board position, sessions, plans, pools, goals, usage, and recovery metadata.

## Major Packages

| Package | Responsibility |
|---|---|
| `internal/types` | Shared data contracts used across backend and Wails bindings. |
| `internal/store` | SQLite persistence, migrations, session usage, goals, plans, pool usage. |
| `internal/workspace` | Workspace registry and onboarding checks. |
| `internal/github` | GitHub API client and polling. |
| `internal/eventbus` | In-process pub/sub for state transitions. |
| `internal/orchestrator` | Goal-aware backlog ranking and auto-pull. |
| `internal/workerpool` | Pool capacity, roles, priorities, and acquisition/release. |
| `internal/session` | Session lifecycle, subprocess spawning, harness dispatch, transcript parsing. |
| `internal/harness` | Tool-calling loop for providers without a native subprocess agent CLI. |
| `internal/llm` | Provider interface and provider drivers. |
| `internal/skills/bundle` | Canonical bundled skill markdown. |
| `frontend/src/components` | User interface. |

## Execution Strategies

PrismConductor supports providers through one provider interface and two execution strategies.

### Subprocess Strategy

A provider implements `SpawnArgs(pool, prompt)` and returns argv. The session manager starts the process with stdout/stderr pointed at the transcript file.

The prompt passed to subprocess providers is self-contained:

1. canonical skill markdown
2. invocation details
3. instruction to follow the skill contract

This lets subprocess agents work without provider-specific skill forks.

### Harness Strategy

Providers that do not expose a local agent subprocess return `llm.ErrNotSupported` from `SpawnArgs` and implement `ToolChat`.

The session manager starts the in-process harness. The harness receives:

- selected skill markdown as system context
- compact invocation as the user message
- tool definitions for file/shell/task operations

Harness output is serialized into the same transcript shape consumed by the session parser.

## Provider Neutrality

Routing decisions should key on provider behavior, not provider names:

- `SpawnArgs` succeeds: subprocess path
- `SpawnArgs` returns `llm.ErrNotSupported`: harness path
- `ToolChat` support controls harness eligibility

Avoid adding `if provider == X` outside provider drivers unless the behavior is genuinely product-specific.

## Skills

Bundled skills are canonical workflow contracts. They are not provider forks.

Current model:

- one `conductor-plan`
- one `conductor-execute`
- one `conductor-continue`
- one `conductor-close`
- one `conductor-question`
- optional pipeline skills such as review, fanout, and conflict resolution

See [Agent-Neutral Skills](agent-neutral-skills.md).

## Workspace And Worktree Flow

Plan sessions run in the workspace repo path.

Execute sessions run in a conductor-managed worktree:

```text
<repo>/.prismconductor/worktrees/<workspace-id>-<issue-number>/
```

The conductor creates the branch and worktree before the worker starts. Plan JSON and answer JSON are mirrored into the worktree so skills can read them through relative paths.

Failed execute worktrees are preserved where possible so users can recover paid-for work.

## Event And Polling Model

The GitHub poller is the external synchronization loop. It detects issue/PR/check/comment/conflict state and emits events.

The orchestrator responds to events. It should not introduce its own polling loops.

Frontend state updates through Wails calls and emitted events.

## Remote Execution

Remote workspace schema and worker code still exist, but remote execution is paused. Local spawn paths return `ErrRemoteWorkspacePaused` for workspaces with `execution_target == "remote"`.

Current product usage should assume local workspaces.
