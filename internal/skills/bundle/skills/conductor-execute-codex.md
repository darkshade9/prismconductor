---
name: conductor-execute-codex
description: Codex-adapted execute skill. Implements an approved plan on a feature branch, verifies the build, commits and pushes, then opens a draft PR. Designed for the codex CLI subprocess provider which uses its own built-in agent loop.
---

# conductor-execute-codex

Bundled by PrismConductor (PRISMCONDUCTOR_PLAN.md §15.7, issue #281).

> NOTE (issue #281): This skill is the codex-adapted variant of `conductor-execute`. It is
> functionally identical in output contract — same commit/push/PR flow, same sentinels — but
> written as a self-contained task description suitable for an autonomous agent CLI. Use this
> as the workspace execute skill when the pool's provider is `codex`.
>
> The conductor spawns this skill inside a per-execute git worktree off `origin/<BASE>`.
> The branch is pre-created. The user's primary checkout is unaffected.

## Task

You are an autonomous software implementation agent. Your job is to implement an approved plan on a feature branch, verify the build passes, and open a draft pull request.

## Inputs

- `--issue <number>` (required)
- `--revision <N>` (required — which approved plan to execute)
- `--repo <path>` (defaults to cwd)
- `--resume-question <id>` (resume mode: continue after a mid-run question was answered)
- `--related-repos <path>` (repeatable; absolute paths to sibling repositories you may grep / read for context)

Parse these from the arguments you were invoked with.

## Behavior

These steps are mandatory. Do not edit files until you are on the feature branch.

### 1. Setup

1. Read `.prismconductor/plans/<issue>-rev<N>.json` (the approved plan).
2. Read `.prismconductor/answers/<issue>-rev<N>.json` (the user's question answers, if any).
3. Read `CLAUDE.md`, `.claude/rules/`, and understand the project's build/lint/test commands.

### 2. Branch hygiene

4. Verify you are on a branch named `feat/issue-<num>-<slug>` (pre-created by the conductor):
   ```
   git status --short   # should be empty
   git branch --show-current   # should start with feat/issue-<num>-
   ```
   If either check fails: print `BLOCKED: worktree integrity check failed — <reason>` and stop.

### 3. Implementation

5. Implement every `files_to_modify` entry from the plan.
6. For every `add` intent, write at least one test.

### 4. Verification

Set up a sandboxed data directory:
```bash
export PRISMCONDUCTOR_DATA_DIR="$(mktemp -d)"
```

7. Run lint / typecheck:
   - Go: `go vet ./...`
   - TypeScript: `npx tsc --noEmit`
   Non-zero exit → `BLOCKED: lint failed — <command> exited <code>`.

8. Build: `wails build` (or `go build ./...` for Go-only).
   Non-zero exit → `BLOCKED: build failed — <command> exited <code>`.

9. Run tests: `go test ./...` (or the project's test command).
   Non-zero exit → `BLOCKED: tests failed — <N> failures`.

### 5. Commit + push + PR

10. Stage only modified files (avoid `git add -A`).
11. Commit with a message ending with `Closes #<num>`.
12. Push: `git push -u origin HEAD`.
13. Create draft PR:
    ```
    gh pr create --draft --base <BASE> --title "<subject> (#<num>)" --body "..."
    ```
    PR body must include: summary, files changed, verification results, `Closes #<num>`.

14. Capture the PR URL from `gh pr create`.

### 6. Sentinels

15. Print `PR_OPENED: <url>` on its own line.
16. Print `Work complete.` on its own line.

## Failure

For any blocking failure:
- Print `BLOCKED: <one-line reason>` as the last line and stop.
- Do NOT print `PR_OPENED:` or `Work complete.` on a failure path.

## Quota / subscription errors

If the ChatGPT subscription limit is reached during execution:
- Print `BLOCKED: ChatGPT subscription limit reached on <provider-name>; resets at <time>` and stop.
- The conductor translates this into a `subscription_quota` failure with the reset time.
