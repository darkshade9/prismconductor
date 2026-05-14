# PrismConductor - Agent Dev Rules

This file is the agent entry point for the same project guidance that was previously provided in `CLAUDE.md`. Keep both files in sync while legacy agent compatibility is still needed.

The product spec lives in `PRISMCONDUCTOR_PLAN.md`. Treat it as the source of truth for data model, schemas, and phase boundaries. When implementation diverges from the plan, update the plan.

## Hard rules

- **Schemas are contracts.** Section 9.1 (plan JSON) and section 6.4 (Question shape) are wire formats between the worker, the orchestrator, and the UI. Don't change a field name without updating all three plus this file.
- **No polling inside the orchestrator.** Orchestrator only runs in response to events (section 7). The 5-min GitHub fetch is the only polling loop and it emits events on diffs.
- **Bundled mode is the default.** A bare repo with no `.claude/`, no `.codex/`, no `CLAUDE.md`, and no `AGENTS.md` must work day-one (section 15.8). Never add a warning or block on a missing repo enrichment.
- **One active goal at a time.** Enforce in `Store.SetGoalActive` via SQL transaction (section 15.4).
- **PTY pattern matching is plain string contains, not regex** (section 10.3). Patterns live in `internal/session/patterns.go` as constants.

## Layout

- `internal/types/` - cross-package data model (section 6).
- `internal/{eventbus,session,store,workerpool,ollama,orchestrator,workspace,github,skills/bundle}/` - one package per backend concern (section 17). Notifications are now in-app Wails-event toasts emitted directly from `app.go`'s `emitToast` (issue #32); no dedicated package. `internal/remoteworker/` is preserved but paused (#254): all spawn methods return `ErrRemoteWorkspacePaused` for `execution_target=="remote"` workspaces.
- `frontend/src/components/` - Board, Card, Column, PlanModal, QuestionForm, SessionDrawer, GoalPane, WorkspaceSwitcher, Settings.
- `frontend/wailsjs/` - auto-generated bindings; never edit by hand. Run `wails generate module` after adding bound methods. Note: `wails generate module` requires `frontend/dist` to exist (run `wails build` first in a fresh worktree); in a bare worktree, add new entries to `App.js` and `App.d.ts` manually, following the existing alphabetical pattern.

## Build

- `go build ./...` - Go compile check.
- `cd frontend && npx tsc --noEmit` - frontend typecheck.
- `wails build` - full app bundle (~40s).
- `PRISMCONDUCTOR_DATA_DIR=<path>` - overrides the conductor's data directory. Tests, smoke harnesses, and worker verification gates MUST set this to a temp dir; otherwise they can corrupt the user's real `~/Library/Application Support/PrismConductor/conductor.db`. CI enforces that no test code touches the prod path (see `.github/workflows/smoke.yml` and `.github/workflows/build.yml`).

## Advanced / Diagnostics

The Event Diagnostics panel (`EventDiagnostics` component) shows live event-bus ring buffers. It is always visible in `wails dev` (DEV builds). In release builds it is hidden by default and can be enabled two ways:

- **Session-only (chord):** Press `Cmd+Opt+D` on macOS or `Ctrl+Alt+D` on Windows/Linux. The Diagnostics tab appears for the current session only and is gone after a restart.
- **Persistent (Settings):** Open Settings > Advanced > enable "Show Diagnostics tab". This persists to `localStorage` under the key `prismconductor.showDiagnostics` and survives restarts.

Intended for support engineers and power users diagnosing UI/backend state drift.

## Verification skills

- **`/bug-hunter-state`** - scans the conductor's persistent state (SQLite DB, `workspaces.json`, transcripts dir, live process table) for 12 known broken-state patterns and writes a timestamped `{findings}` JSON + Markdown report to `.prismconductor/bug-hunter/`. Read-only; never mutates state. Invoke on demand or schedule with `/loop 24h /bug-hunter-state`. Reports land in `.prismconductor/bug-hunter/<RFC3339>.{json,md}`; `latest.json` always points to the most recent run.
- **`/conductor-skill-curator`** - reads the `skill_outcomes` log for a named skill (e.g. `--skill bundled:conductor-plan`), detects repeated failure patterns, and proposes targeted markdown edits. Read-only; never mutates skill files. Reports land in `.prismconductor/skill-curator/<RFC3339>.{json,md}`; `latest.json` always points to the most recent run. See section 15.12.
