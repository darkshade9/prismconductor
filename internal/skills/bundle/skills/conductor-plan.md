---
name: conductor-plan
description: Universal `/start-issue` analogue. Reads a GitHub issue, greps the repo, reads any CLAUDE.md and .claude/rules/ present, and emits a structured plan JSON to .prismconductor/plans/<issue>-rev<N>.json. Stops at the proposal gate. No code mutation.
---

# conductor-plan

Bundled by PrismConductor. Used in Bundled and Hybrid skill modes (PRISMCONDUCTOR_PLAN.md §15.7).

## Inputs

- `--issue <number>` (required)
- `--repo <path>` (defaults to cwd)
- `--native-cmd <command>` (Hybrid mode: wraps a repo's own planning skill)

## Behavior

1. Fetch the issue body via `gh issue view <number> --json title,body,labels`.
2. Read `CLAUDE.md` and `.claude/rules/*.md` if present (silently skip if absent — see §15.8).
3. Grep the repo for terms in the issue title and body that name files/symbols.
4. Compose a plan that lists:
   - What to do (markdown)
   - Files to add/modify/delete
   - Detected dependencies (other open issue numbers)
   - Open questions to ask the user (structured per §6.4 Question schema)
5. Write the plan to `.prismconductor/plans/<issue>-rev1.json` matching §9.1 exactly.
6. Print `Plan written to .prismconductor/plans/<issue>-rev1.json` so the conductor's PTY parser picks it up (§10.3).
7. Stop. Do not edit code.

## Revisions

If `.prismconductor/answers/<issue>-rev<N>.json` exists, regenerate as rev<N+1> consuming the answers.
