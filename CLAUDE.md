# PrismConductor — Dev Rules

The product spec lives in `PRISMCONDUCTOR_PLAN.md`. Treat it as the source of truth for data model, schemas, and phase boundaries. When implementation diverges from the plan, update the plan.

## Hard rules

- **Schemas are contracts.** §9.1 (plan JSON) and §6.4 (Question shape) are wire formats between the worker, the orchestrator, and the UI. Don't change a field name without updating all three plus this file.
- **No polling inside the orchestrator.** Orchestrator only runs in response to events (§7). The 5-min GitHub fetch is the only polling loop and it emits events on diffs.
- **Bundled mode is the default.** A bare repo with no `.claude/` and no `CLAUDE.md` must work day-one (§15.8). Never add a warning or block on a missing repo enrichment.
- **One active goal at a time.** Enforce in `Store.SetGoalActive` via SQL transaction (§15.4).
- **PTY pattern matching is plain string contains, not regex** (§10.3). Patterns live in `internal/session/patterns.go` as constants.

## Layout

- `internal/types/` — cross-package data model (§6).
- `internal/{eventbus,session,store,workerpool,ollama,orchestrator,workspace,github,skills/bundle}/` — one package per backend concern (§17). Notifications are now in-app Wails-event toasts emitted directly from `app.go`'s `emitToast` (issue #32); no dedicated package.
- `frontend/src/components/` — Board, Card, Column, PlanModal, QuestionForm, SessionDrawer, GoalPane, WorkspaceSwitcher, Settings.
- `frontend/wailsjs/` — auto-generated bindings; never edit by hand. Run `wails generate module` after adding bound methods.

## Build

- `go build ./...` — Go compile check.
- `cd frontend && npx tsc --noEmit` — frontend typecheck.
- `wails build` — full app bundle (~40s).
- `PRISMCONDUCTOR_DATA_DIR=<path>` — overrides the conductor's data directory. Tests, smoke harnesses, and worker verification gates MUST set this to a temp dir; otherwise they can corrupt the user's real `~/Library/Application Support/PrismConductor/conductor.db`. CI enforces that no test code touches the prod path (see `.github/workflows/smoke.yml` and `.github/workflows/build.yml`).

## Advanced / Diagnostics

The Event Diagnostics panel (`EventDiagnostics` component) shows live event-bus ring buffers. It is always visible in `wails dev` (DEV builds). In release builds it is hidden by default and can be enabled two ways:

- **Session-only (chord):** Press `Cmd+Opt+D` on macOS or `Ctrl+Alt+D` on Windows/Linux. The Diagnostics tab appears for the current session only and is gone after a restart.
- **Persistent (Settings):** Open Settings → Advanced → enable "Show Diagnostics tab". This persists to `localStorage` under the key `prismconductor.showDiagnostics` and survives restarts.

Intended for support engineers and power users diagnosing UI/backend state drift.

## Verification skills

- **`/bug-hunter-state`** — scans the conductor's persistent state (SQLite DB, `workspaces.json`, transcripts dir, live process table) for 12 known broken-state patterns and writes a timestamped `{findings}` JSON + Markdown report to `.prismconductor/bug-hunter/`. Read-only; never mutates state. Invoke on demand or schedule with `/loop 24h /bug-hunter-state`. Reports land in `.prismconductor/bug-hunter/<RFC3339>.{json,md}`; `latest.json` always points to the most recent run.
