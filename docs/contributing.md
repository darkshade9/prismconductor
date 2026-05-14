# Contributing Guide

This guide is for people changing PrismConductor itself. For product usage, see [User Guide](user-guide.md).

## Project Principles

PrismConductor is vendor- and agent-neutral. Provider-specific behavior belongs behind provider drivers and invocation adapters, not inside product workflows.

Keep these constraints in mind:

- GitHub issues and PRs remain the external system of record.
- Bare repositories must work without repo-local agent files.
- Bundled skills must be canonical and provider-neutral.
- Schemas are contracts between agents, backend, and UI.
- The orchestrator is event-driven; do not add hidden polling loops outside the GitHub poller.
- Failed agent work should be recoverable when possible.

Current architecture and contracts live in [Architecture](architecture.md), [Data Contracts](data-contracts.md), and [Agent-Neutral Skills](agent-neutral-skills.md). Short operational rules live in [AGENTS.md](../AGENTS.md). The old [PRISMCONDUCTOR_PLAN.md](../PRISMCONDUCTOR_PLAN.md) is retained as a historical bootstrap plan and rationale archive.

## Local Development Setup

Install:

| Tool | Purpose |
|---|---|
| Go | Backend, Wails bindings, tests. |
| Node | Frontend dependencies and typecheck. |
| Wails v2 | Desktop shell and dev server. |
| GitHub CLI (`gh`) | Runtime issue and PR integration. |
| Git | Worktrees, branches, merge helpers. |

Typical setup:

```bash
git clone https://github.com/darkshade9/prismconductor.git
cd prismconductor
cd frontend && npm install && cd ..
go install github.com/wailsapp/wails/v2/cmd/wails@latest
```

Run the app:

```bash
wails dev
```

Build a release-style binary:

```bash
wails build
```

## Repository Layout

| Path | Purpose |
|---|---|
| `app.go` | Wails app façade and high-level backend wiring. |
| `internal/types/` | Cross-package data contracts. |
| `internal/store/` | SQLite persistence and migrations. |
| `internal/session/` | Session manager for subprocess and harness work. |
| `internal/harness/` | Tool-calling loop for non-subprocess providers. |
| `internal/llm/` | Provider interfaces and provider drivers. |
| `internal/workerpool/` | Pool capacity, routing, and role selection. |
| `internal/orchestrator/` | Goal-aware backlog ranking and auto-pull. |
| `internal/github/` | GitHub polling and API integration. |
| `internal/skills/bundle/skills/` | Canonical bundled workflow skills. |
| `frontend/src/components/` | React UI components. |
| `frontend/wailsjs/` | Generated Wails bindings; do not edit by hand. |
| `worker/` | Cloudflare Worker code for the paused remote path. |

## Architecture

### Provider Boundary

Provider-specific behavior lives behind `internal/llm.Provider`.

There are two execution strategies:

- **Subprocess providers** return argv from `SpawnArgs`. The session manager starts the CLI and passes a self-contained prompt containing the canonical skill markdown and invocation.
- **Harness providers** return `llm.ErrNotSupported` from `SpawnArgs` and implement `ToolChat`. The harness receives the canonical skill markdown as system context and the invocation as the user message.

Do not fork skills by provider. Add or update provider adapters instead.

### Skills

Bundled skills are workflow contracts. They must describe:

- inputs
- required reads/writes
- sentinels
- schemas
- verification gates
- stop/block behavior

They must not encode provider-specific execution mechanics. See [Agent-Neutral Skills](agent-neutral-skills.md).

### Workspaces And Worktrees

Workspaces point at local git checkouts. Execute sessions run in conductor-managed worktrees under `.prismconductor/worktrees/` so the user’s main checkout remains untouched.

Plan artifacts and answers are mirrored into execute worktrees because `.prismconductor/` files are not necessarily tracked by git.

### State And Events

Persistent state is split across:

- workspace registry files
- SQLite store
- transcripts
- repo-local `.prismconductor/` artifacts

Use event bus flows for state changes. Avoid direct cross-package polling except where an existing poller owns external synchronization.

## Verification

Run targeted tests while developing, then broader checks before opening a PR.

```bash
go test ./...
cd frontend && npx tsc --noEmit
```

For smoke tests:

```bash
cd tests
npm install
npm run smoke:install
npm run smoke
```

When tests touch PrismConductor runtime state, set:

```bash
PRISMCONDUCTOR_DATA_DIR="$(mktemp -d)"
```

Never let tests write to the user’s real application support directory.

## Wails Bindings

After adding or changing exported Wails methods, regenerate bindings:

```bash
wails generate module
```

If `frontend/dist` does not exist in a fresh worktree, run `wails build` first or update `frontend/wailsjs/go/main/App.{js,d.ts}` manually following the existing alphabetical pattern.

## Database Changes

For SQLite changes:

1. Add a migration in `internal/store/migrations`.
2. Keep migrations idempotent.
3. Add tests around upgrade behavior.
4. Preserve backward compatibility for existing local data where possible.

## Frontend Changes

Use existing UI patterns before introducing new ones:

- Settings tabs live in `frontend/src/components/Settings.tsx`.
- Workspace settings live in `WorkspacesPanel.tsx`.
- Provider credentials live in `ProvidersPanel.tsx`.
- Pool routing/cost lives in `PoolsPanel.tsx`.
- Issue cards live in `Card.tsx`.
- Plan approval lives in `PlanModal.tsx`.

Keep operational screens dense, predictable, and suitable for repeated use.

## Documentation Changes

Update docs in the same PR when changing user-visible behavior.

| Change | Docs to update |
|---|---|
| User workflow | `docs/user-guide.md` |
| Provider/pool behavior | `docs/user-guide.md`, `docs/contributing.md` |
| Skill contract | `docs/agent-neutral-skills.md`, affected skill markdown |
| Remote execution | `docs/remote-workspaces.md` |
| Cost display | `docs/cost-expectations.md` |
| Build/test flow | `docs/contributing.md`, `README.md` |

## Pull Request Checklist

- The change follows existing package boundaries.
- User-facing behavior is documented.
- `go test ./...` passes, or the PR explains a narrower validated set.
- `cd frontend && npx tsc --noEmit` passes for frontend changes.
- Wails bindings are regenerated when needed.
- Bundled skills remain provider-neutral and pass bundle tests.
- No unrelated generated files, local database files, transcripts, or worktrees are committed.
