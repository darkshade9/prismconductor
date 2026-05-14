# Roadmap

This roadmap reflects current product direction. The older phased launch plan in `PRISMCONDUCTOR_PLAN.md` is historical.

## Current Product Pillars

- Local-first multi-workspace issue orchestration.
- Vendor- and agent-neutral worker execution.
- Canonical bundled skills maintained once.
- Human review gates for plans, questions, and PR merges.
- Recoverable worktrees and transparent cost/session telemetry.

## Active / Current

| Area | Direction |
|---|---|
| Local workspaces | Primary supported execution mode. |
| Provider model | Continue expanding provider support through `internal/llm.Provider` without forking skills. |
| Skills | Keep bundled skills canonical and agent-neutral. |
| Cost visibility | Improve pool/session/goal spend projections and rate configuration. |
| Recovery | Preserve failed work and improve NEEDS_PR, conflict, and self-heal flows. |
| Docs | Keep user/contributor docs current with actual code behavior. |

## Paused

| Area | Status |
|---|---|
| Remote workspaces | Schema and worker code remain, but execution is paused while auth/execution safety is revisited. |

## Candidate Next Investments

- Current-state architecture diagrams in docs.
- Stronger plan schema documentation generated from Go structs.
- Provider capability matrix in the UI and docs.
- Better workspace onboarding checks for non-Claude agent instructions.
- Skill outcome review UI for applying curator suggestions.
- More explicit budget controls, not just projections.
- Broader frontend component tests for Settings and Plan workflows.

## Principles For Roadmap Decisions

- Do not compromise vendor neutrality for one provider shortcut.
- Prefer runner adapters over duplicate skill files.
- Keep PM workflows understandable without reading code.
- Keep engineer workflows recoverable and auditable.
- Add automation only where the human approval boundary remains clear.
