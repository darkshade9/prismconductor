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
- `internal/{eventbus,session,store,workerpool,ollama,orchestrator,workspace,github,notify,skills/bundle}/` — one package per backend concern (§17).
- `frontend/src/components/` — Board, Card, Column, PlanModal, QuestionForm, SessionDrawer, GoalPane, WorkspaceSwitcher, Settings.
- `frontend/wailsjs/` — auto-generated bindings; never edit by hand. Run `wails generate module` after adding bound methods.

## Build

- `go build ./...` — Go compile check.
- `cd frontend && npx tsc --noEmit` — frontend typecheck.
- `wails build` — full app bundle (~40s).
