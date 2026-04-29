---
name: conductor-execute
description: Resumes an issue from an approved plan plus answered questions. Implements the plan on a fresh feature branch, commits, pushes, and opens a draft PR linking back to the issue. Never edits the default branch directly.
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
3. Read repo conventions: `CLAUDE.md`, `.claude/rules/`, and any test/build/lint commands the conductor injected via env (`PRISMCONDUCTOR_TEST_CMD`, etc.).

### 2. Branch hygiene (NON-NEGOTIABLE)

4. Determine the default branch via `gh repo view --json defaultBranchRef -q .defaultBranchRef.name` (or `git symbolic-ref refs/remotes/origin/HEAD` as fallback). Call it `BASE`.
5. Refuse to operate on a dirty working tree. If `git status --porcelain` is non-empty:
   - Print `BLOCKED: working tree has uncommitted changes; resolve before re-running` and exit. Do not stash, do not commit, do not delete.
6. `git fetch origin` then `git switch -c feat/issue-<num>-<slug> origin/<BASE>`. The slug is a kebab-case truncation of the issue title (≤40 chars). If the branch already exists locally or remotely, use `feat/issue-<num>-<slug>-<short-uuid>` instead — never overwrite an existing branch.

### 3. Implementation

7. Implement the plan: edit files listed in `files_to_modify`, write tests for `add` intents, follow the conventions discovered in step 3.
8. Run the test command if configured. Skip silently if not — note "No tests run (no runner detected)" in the PR body (§15.8). Run any lint/format command the repo configures.

### 4. Commit + push + PR (NON-NEGOTIABLE)

9. `git add` only the files you modified (avoid `git add -A` — never commit unrelated WIP that may be in the tree from another task).
10. `git commit -m "<short subject>\n\nCloses #<num>\n\nCo-Authored-By: PrismConductor worker <noreply@anthropic.com>"`. Subject must reference the issue number; body must include `Closes #<num>` so the PR auto-closes the issue on merge.
11. `git push -u origin HEAD`.
12. `gh pr create --draft --base <BASE> --title "<short subject> (#<num>)" --body-file -` — pipe a body containing:
    - A 1-2 sentence summary
    - The list of files modified
    - Test/lint outcomes
    - Anything skipped or flagged for human review
    - The literal trailer line `Closes #<num>`
13. Capture the PR URL from the `gh pr create` output.

### 5. Sentinel for the conductor

14. Print exactly `PR_OPENED: <url>` on its own line so the conductor can parse the PR number and move the card to REVIEW.
15. Print `Work complete.` so the PTY parser advances state (§10.3).

### Failure paths

- If any of steps 4, 9-12 fail (network down, no remote, push rejected, etc.), print `BLOCKED: <one-line reason>` and exit. Leave the branch in place so the user can finish manually.
- If tests fail in step 8, still commit + push + open the PR but mark it draft and surface the failure prominently in the body. Do not silently swallow.

## Mid-execution questions

If the agent gets stuck on something the plan didn't cover, invoke `/conductor-question` (writes a structured question, pauses for answer). Do this before step 4 if possible — once a feature branch exists, you should drive the work to completion.
