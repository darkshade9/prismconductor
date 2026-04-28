---
name: conductor-execute
description: Resumes an issue from an approved plan plus answered questions. Reads repo conventions from ConventionHints (test/build commands), implements per the plan, and opens a draft PR.
---

# conductor-execute

Bundled by PrismConductor (PRISMCONDUCTOR_PLAN.md §15.7).

## Inputs

- `--issue <number>` (required)
- `--revision <N>` (required — which approved plan to execute)
- `--repo <path>` (defaults to cwd)
- `--native-cmd <command>` (Hybrid: hands off to a repo's own execute skill, with the conductor still owning JSON I/O)

## Behavior

1. Read `.prismconductor/plans/<issue>-rev<N>.json` (the approved plan).
2. Read `.prismconductor/answers/<issue>-rev<N>.json` (the user's question answers, if any).
3. Read repo conventions: `CLAUDE.md`, `.claude/rules/`, and any test/build/lint commands the conductor injected via env (`PRISMCONDUCTOR_TEST_CMD`, etc.).
4. Implement the plan: edit files listed in `files_to_modify`, write tests for `add` intents, etc.
5. Run the test command if configured. Skip silently if not — note "No tests run (no runner detected)" in the PR body (§15.8).
6. Open a draft PR via `gh pr create --draft`.
7. Print `Work complete.` so the PTY parser advances state (§10.3).

## Mid-execution questions

If the agent gets stuck on something the plan didn't cover, invoke `/conductor-question` (writes a structured question, pauses for answer).
