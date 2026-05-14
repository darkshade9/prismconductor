---
name: conductor-resolve-conflicts
description: Resolves merge or rebase conflicts on a PrismConductor PR branch using the conductor-provided runbook, verifies the result, commits targeted conflict-resolution changes, and refuses unsafe dirty-worktree states.
---

# conductor-resolve-conflicts

Bundled by PrismConductor (PRISMCONDUCTOR_PLAN.md §15.7).

You are a conflict-resolution worker. Your task is to resolve merge conflicts on a PR branch so it can be cleanly merged into its base branch.

## Inputs (injected by the conductor)

The worker note you receive will contain:
- The PR head branch name and base branch name
- A list of files changed by the PR (candidates for conflicts)
- The resolution runbook (rebase strategy)

## Behavior

### 1. Verify state

```bash
git status --short          # must be clean before starting
git branch --show-current   # must match the PR head branch
```

If the working tree is dirty or the branch doesn't match, print `BLOCKED: worktree not clean — manual intervention required` and exit.

### 2. Fetch base

```bash
git fetch origin
```

### 3. Attempt rebase

```bash
git rebase origin/<BASE>
```

If the rebase succeeds cleanly (exit 0, no conflicts), skip to step 5.

### 4. Resolve conflicts

For each conflicted file:

1. Read the conflict markers carefully.
2. Resolve **conservatively**: keep the upstream (base) intent intact; incorporate the PR changes only where they don't conflict semantically.
3. If a file is too complex to resolve safely (e.g., deeply interleaved logic), emit:
   ```
   BLOCKED: <filename> — cannot safely resolve: <one-line reason>
   ```
   Then run `git rebase --abort` and exit. Do **not** commit partial resolutions.
4. After resolving each file: `git add <filename>`
5. Continue: `git rebase --continue`

### 5. Verify

Run the project's test and lint commands (from repo agent instructions or `PRISMCONDUCTOR_BUILD_CMD` / `PRISMCONDUCTOR_TEST_CMD`):

- Non-zero lint exit → `BLOCKED: lint failed after conflict resolution — <command> exited <code>`
- Non-zero test exit → `BLOCKED: tests failed after conflict resolution — <N> failures`

If verification passes, continue.

### 6. Force-push

```bash
git push --force-with-lease
```

If the push is rejected (e.g., upstream was force-pushed since we started):

```
BLOCKED: force-push rejected — upstream branch changed while resolving; re-run to retry
```

### 7. Post PR comment

Use the `gh` CLI to post a summary comment on the PR:

```bash
gh pr comment <PR_NUMBER> --body "Conflict resolution applied via PrismConductor. Rebased onto <BASE>. Linters and tests pass."
```

### 8. Signal completion

Print `Work complete.` so the conductor advances the card state.

## Failure paths

Any unrecoverable error must print `BLOCKED: <reason>` as the last line. Never print `Work complete.` on a failure path.

## Notes

- Never rebase across force-pushes from a third party — detect with `git fetch origin && git status` and abort if HEAD diverged.
- Never amend commits on `main` / `master` / the default branch.
- If the conflict involves generated files (e.g., `go.sum`, lock files), regenerate them (`go mod tidy`, `npm install`) rather than manually editing conflict markers.
