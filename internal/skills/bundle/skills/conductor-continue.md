---
name: conductor-continue
description: Continues work on an existing feature branch after review feedback or test failures. Reads the original plan and a user note describing what to fix, makes targeted edits, and pushes commits to the same PR branch. Never creates a new branch.
---

# conductor-continue

Bundled by PrismConductor.

> This skill runs inside an **existing** git worktree for the feature branch.
> The branch and worktree already exist on disk. Do NOT create a new branch.
> Do NOT `git stash`, do NOT `git reset --hard`, do NOT switch branches.
> The user has asked you to continue work on an open PR by applying targeted
> changes described in their note.

## Inputs

- `--issue <number>` (required)
- `--revision <N>` (required — the approved plan revision to reference for context)
- `--repo <path>` (defaults to cwd)

The user's note describing what to fix is at `.prismconductor/notes/<issue>.txt`
in the worktree root. Read it first — it is your primary directive.

**CI self-heal mode:** If the note begins with `CI self-heal:`, the session was
auto-spawned by the self-heal loop (issue #116). In this case:
- Run the failing checks listed in the note locally to reproduce them.
- Fetch the failing step logs via `gh run view <run-id> --log-failed` if run IDs
  are available in the note, or use the check run URLs to identify the workflow.
- Review the diff of the HEAD commit (`git diff HEAD~1`) for context.
- Fix the root cause and push a fixup commit.
- Comment on the PR: "Self-heal: tests were failing because X; fixed by Y."
- If you cannot reproduce the failure locally or it is environment-only,
  print `BLOCKED: self-heal cannot fix environment-only CI failure — <reason>`.
  Do NOT push a commit that doesn't address the actual root cause.

## Behavior

These steps are mandatory and must run in this order.

### 1. Setup

1. Read `.prismconductor/notes/<issue>.txt` — the user's description of what
   to fix (required). If absent, print
   `BLOCKED: continue note missing at .prismconductor/notes/<issue>.txt` and exit.
2. Read `.prismconductor/plans/<issue>-rev<N>.json` — original plan for context
   (understand what was being built).
3. Read `.prismconductor/answers/<issue>-rev<N>.json` (user's question answers,
   if any — optional).
4. Read repo conventions: `AGENTS.md`, `CLAUDE.md`, `.codex/rules/`, `.claude/rules/`.

### 2. Branch hygiene (NON-NEGOTIABLE)

5. Sanity check only — do NOT modify branch state:
   ```
   git status --short
   git branch --show-current     # must start with feat/issue-<num>-
   ```
   If `git branch --show-current` does not start with `feat/issue-<num>-`:
   print `BLOCKED: worktree integrity check failed — branch is <actual>,
   expected feat/issue-<num>-*` and exit. Do not attempt to recover.

### 3. Implementation

6. Review the user's note carefully. Understand what specifically needs to be
   fixed (failing tests, lint errors, review feedback, etc.).
7. Run the relevant checks first to confirm the current state:
   - If the note mentions failing tests: run the test suite and read the output.
   - If the note mentions lint errors: run the linter and read the output.
   - Read the relevant files before editing.
8. Make **targeted, minimal edits** to address the note. Do NOT invent
   unrelated changes, refactor beyond the note's scope, or modify files not
   relevant to the fix.
9. If you hit a question the note didn't cover, invoke `/conductor-question`
   before staging any changes.

### 4. Verification gates (NON-NEGOTIABLE — block on failure)

These run before commit. Failures are stop-the-world events.

10. **Lint / typecheck** every changed surface:
    - Go files touched → `go vet ./...` (or `PRISMCONDUCTOR_LINT_CMD` if set).
    - TypeScript files touched → `npx tsc --noEmit` from the relevant frontend dir.
    - Any non-zero exit → `BLOCKED: lint failed — <command> exited <code>` and exit.
11. **Build** the project:
    - Wails project: `wails build` from repo root.
    - Go-only: `go build ./...`.
    - Non-zero exit → `BLOCKED: build failed — <command> exited <code>` and exit.
12. **Run tests**:
    - `PRISMCONDUCTOR_TEST_CMD` if set, else `go test ./...` for Go, `npm test` for Node.
    - Non-zero exit → `BLOCKED: tests failed — <N> failures; full output below` then dump last 50 lines and exit.

### 5. Commit + push + PR (NON-NEGOTIABLE)

13. `git add` only the files you modified (never `git add -A`).
14. Commit with message:
    ```
    chore(issue-<num>): continue work — <first line of user note>

    Closes #<num>

    Co-Authored-By: PrismConductor worker <noreply@anthropic.com>
    ```
15. `git push` to the current branch (do NOT force-push).
16. **Single-PR enforcement:** run `gh pr list --head <branch> --json number,url`.
    - If a PR already exists: post a follow-up comment summarizing this leg's
      work via `gh pr comment <num> --body "..."`. Use the existing PR URL.
      Do NOT open a new PR.
    - If no PR exists (defensive case): open one with
      `gh pr create --draft --title "Continue: <first line of note> (#<num>)" --body "..."`.
17. Capture the PR URL.

### 6. Sentinels

18. Print `PR_OPENED: <url>` on its own line (same URL as the existing PR).
19. Print `Work complete.`

### Failure paths (NON-NEGOTIABLE)

If you cannot complete this skill for ANY reason, your last printed line MUST
start with `BLOCKED: ` followed by a one-line reason.

`Work complete.` and `PR_OPENED:` are reserved for the success path only —
never print them on a failure.
