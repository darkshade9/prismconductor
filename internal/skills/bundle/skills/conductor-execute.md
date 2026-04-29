---
name: conductor-execute
description: Resumes an issue from an approved plan plus answered questions. Implements the plan on a fresh feature branch, lints, runs tests, refuses to commit if anything fails, then commits, pushes, and opens a draft PR linking back to the issue. Never edits the default branch directly.
---

# conductor-execute

Bundled by PrismConductor (PRISMCONDUCTOR_PLAN.md §15.7).

## Inputs

- `--issue <number>` (required)
- `--revision <N>` (required — which approved plan to execute)
- `--repo <path>` (defaults to cwd)
- `--native-cmd <command>` (Hybrid: hands off to a repo's own execute skill, with the conductor still owning JSON I/O)

## Behavior

These steps are mandatory and must run in this order. Do **not** edit any files in step 5 until step 4 has placed you on a fresh feature branch.

### 1. Setup

1. Read `.prismconductor/plans/<issue>-rev<N>.json` (the approved plan).
2. Read `.prismconductor/answers/<issue>-rev<N>.json` (the user's question answers, if any).
3. Read repo conventions: `CLAUDE.md`, `.claude/rules/`, and any test/build/lint commands the conductor injected via env (`PRISMCONDUCTOR_TEST_CMD`, `PRISMCONDUCTOR_LINT_CMD`, `PRISMCONDUCTOR_BUILD_CMD`).

### 2. Branch hygiene (NON-NEGOTIABLE)

4. Determine the default branch via `gh repo view --json defaultBranchRef -q .defaultBranchRef.name` (or `git symbolic-ref refs/remotes/origin/HEAD` as fallback). Call it `BASE`.
5. Refuse to operate on a dirty working tree. If `git status --porcelain` is non-empty:
   - Print `BLOCKED: working tree has uncommitted changes; resolve before re-running` and exit. Do not stash, do not commit, do not delete.
6. `git fetch origin` then `git switch -c feat/issue-<num>-<slug> origin/<BASE>`. The slug is a kebab-case truncation of the issue title (≤40 chars). If the branch already exists locally or remotely, use `feat/issue-<num>-<slug>-<short-uuid>` instead — never overwrite an existing branch.

### 3. Implementation

7. Implement the plan: edit files listed in `files_to_modify`, follow the conventions discovered in step 3.
8. **For every `add` intent in the plan, write at least one test exercising the new code.** If the plan adds a Go function `Foo`, add a `TestFoo` (or table test) in the appropriate `_test.go`. If the plan adds a React component or hook, add a Vitest/Jest test (or at minimum a render-doesn't-throw smoke test). Skipping this with "no test framework yet" is only acceptable if the repo genuinely has no test infrastructure AND you note it explicitly in the PR body.

### 4. Verification gates (NON-NEGOTIABLE — block on failure)

These run before commit. Failures here are **stop-the-world** events: print `BLOCKED:` and exit, leaving the branch in place for the user to inspect. Do **not** commit broken work. Do **not** swallow failures into the PR body.

9. **Lint / typecheck** every changed surface:
   - Go files touched → `go vet ./...` (or the project's `PRISMCONDUCTOR_LINT_CMD` if set).
   - TypeScript files touched → `npx tsc --noEmit` from the relevant frontend dir.
   - Other languages → run the project-conventional linter from `CLAUDE.md`.
   Any non-zero exit → `BLOCKED: lint failed — <command> exited <code>` and exit.
10. **Build** the project:
    - For a Wails project: `wails build` from repo root.
    - For Go-only: `go build ./...`.
    - For Node-only: `npm run build` (or pnpm/yarn equivalent).
    Non-zero exit → `BLOCKED: build failed — <command> exited <code>` and exit.
11. **Run tests**:
    - If `PRISMCONDUCTOR_TEST_CMD` is set, run it. Else fall back to `go test ./...` for Go projects, `npm test` (or vitest/jest) for Node, `pytest` for Python.
    - Non-zero exit → `BLOCKED: tests failed — <N> failures; full output below` then dump the last 50 lines of test output and exit.
    - If no test runner is available, you MAY proceed but must include `## ⚠ No tests run (no runner detected)` as a top-level header in the PR body. This is the ONLY case where a missing test step is acceptable.

### 5. Commit + push + PR (NON-NEGOTIABLE)

12. `git add` only the files you modified (avoid `git add -A` — never commit unrelated WIP that may be in the tree from another task).
13. `git commit -m "<short subject>\n\nCloses #<num>\n\nCo-Authored-By: PrismConductor worker <noreply@anthropic.com>"`. Subject must reference the issue number; body must include `Closes #<num>` so the PR auto-closes the issue on merge.
14. `git push -u origin HEAD`.
15. `gh pr create --draft --base <BASE> --title "<short subject> (#<num>)" --body-file -` — pipe a body containing:
    - A 1-2 sentence summary
    - The list of files modified
    - **Verification:** lint command + result, build command + result, test command + result with pass/fail counts
    - Anything skipped or flagged for human review
    - The literal trailer line `Closes #<num>`
16. Capture the PR URL from the `gh pr create` output.

### 6. Sentinels for the conductor

17. Print exactly `PR_OPENED: <url>` on its own line so the conductor can parse the PR number and move the card to TESTING.
18. Print `Work complete.` so the PTY parser advances state (§10.3).

### Failure paths

- Steps 4-6 (branch hygiene) failures → `BLOCKED: <reason>`. No branch created, nothing to clean up.
- Step 9-11 (verification) failures → `BLOCKED: <reason>`. Branch exists with WIP code so the user can `git switch <branch>` and finish manually. Do not push.
- Steps 12-15 (commit/push/PR) failures (network down, no remote, push rejected, etc.) → `BLOCKED: <reason>`. Branch + commit exist locally.

## Mid-execution questions

If the agent gets stuck on something the plan didn't cover, invoke `/conductor-question` (writes a structured question, pauses for answer). Do this before step 4 if possible — once a feature branch exists, you should drive the work to completion.
