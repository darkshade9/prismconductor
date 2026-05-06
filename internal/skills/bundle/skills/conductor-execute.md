---
name: conductor-execute
description: Resumes an issue from an approved plan plus answered questions. Implements the plan on a fresh feature branch, lints, runs tests, refuses to commit if anything fails, then commits, pushes, and opens a draft PR linking back to the issue. Never edits the default branch directly.
---

# conductor-execute

Bundled by PrismConductor (PRISMCONDUCTOR_PLAN.md §15.7).

> Note (issue #22): the conductor spawns this skill inside a per-execute git
> worktree off `origin/<BASE>`. The branch is pre-created. The user's primary
> checkout is unaffected by anything you do here. Multiple parallel execute
> workers each run in their own worktree.

## Inputs

- `--issue <number>` (required)
- `--revision <N>` (required — which approved plan to execute)
- `--repo <path>` (defaults to cwd)
- `--native-cmd <command>` (Hybrid: hands off to a repo's own execute skill, with the conductor still owning JSON I/O)
- `--resume-question <id>` (#17 — resume mode: continue an earlier execute on the same branch after a mid-run question was answered. When set, the steps below diverge as noted under **Resume mode**.)
- `--related-repos <path>` (repeatable; absolute paths to sibling repositories you may grep / read for context)

## Sibling repos (read-only)

If `--related-repos` is set: the listed paths are SIBLING repositories you may grep / read for context. **Do NOT modify any file under those paths.** Use them to understand cross-repo contracts, naming conventions, or how the sibling code calls into yours. Reference findings in commits and the PR body when relevant.

## Behavior

These steps are mandatory and must run in this order. Do **not** edit any files in step 5 until step 4 has placed you on a fresh feature branch.

### 1. Setup

1. Read `.prismconductor/plans/<issue>-rev<N>.json` (the approved plan).
2. Read `.prismconductor/answers/<issue>-rev<N>.json` (the user's question answers, if any).
3. Read repo conventions: `CLAUDE.md`, `.claude/rules/`, and any test/build/lint commands the conductor injected via env (`PRISMCONDUCTOR_TEST_CMD`, `PRISMCONDUCTOR_LINT_CMD`, `PRISMCONDUCTOR_BUILD_CMD`).

### 2. Branch hygiene (NON-NEGOTIABLE)

4. Determine the default branch via `gh repo view --json defaultBranchRef -q .defaultBranchRef.name` (or `git symbolic-ref refs/remotes/origin/HEAD` as fallback). Call it `BASE`.
5. **You are running inside a conductor-managed worktree.** The conductor has
   already fetched origin, created a fresh `feat/issue-<num>-<slug>` branch off
   `origin/<BASE>`, and put you on it. Do NOT `git switch` to a different
   branch. Do NOT `git stash`, do NOT `git reset` — the working tree was just
   created clean. Sanity check ONLY:
   ```
   git status --short            # should be empty (or contain only the prior
                                 #   worker's edits, in resume mode)
   git branch --show-current     # should start with feat/issue-<num>-
   ```
   If either check fails: print `BLOCKED: worktree integrity check failed — <reason>` and exit. Do not attempt to recover.

### 3. Implementation

7. Implement the plan: edit files listed in `files_to_modify`, follow the conventions discovered in step 3.
8. **For every `add` intent in the plan, write at least one test exercising the new code.** If the plan adds a Go function `Foo`, add a `TestFoo` (or table test) in the appropriate `_test.go`. If the plan adds a React component or hook, add a Vitest/Jest test (or at minimum a render-doesn't-throw smoke test). Skipping this with "no test framework yet" is only acceptable if the repo genuinely has no test infrastructure AND you note it explicitly in the PR body.

If you hit a question the plan didn't cover, invoke `/conductor-question` (writes a structured question, prints `QUESTION_PENDING: <id>`, exits 0). The conductor pauses the card on `paused_for_question`, surfaces the question to the user, and re-spawns this skill with `--resume-question <id>` once they answer. **Use this BEFORE you start committing partial work** — once you've staged changes, drive the work to completion instead.

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
10b. **Smoke test (UI runtime regression gate):** if `tests/e2e/startup.spec.ts` exists in the repo, run `npx --prefix tests playwright test tests/e2e/startup.spec.ts`. Non-zero exit → `BLOCKED: smoke test failed — UI did not mount cleanly; transcript above` and exit, leaving the branch in place. If the spec file does NOT exist (older feature branches / non-Wails workspaces), print `⚠ smoke test skipped — tests/e2e/startup.spec.ts not present in this repo` to the PTY (visible to the user and surfaced in the PR body's verification section) and continue with step 11. Do NOT block.
11. **Run tests**:
    - If `PRISMCONDUCTOR_TEST_CMD` is set, run it. Else fall back to `go test ./...` for Go projects, `npm test` (or vitest/jest) for Node, `pytest` for Python.
    - Non-zero exit → `BLOCKED: tests failed — <N> failures; full output below` then dump the last 50 lines of test output and exit.
    - If no test runner is available, you MAY proceed but must include `## ⚠ No tests run (no runner detected)` as a top-level header in the PR body. This is the ONLY case where a missing test step is acceptable.

### 5. Commit + push + PR (NON-NEGOTIABLE)

The commit+push step uses a **layered fallback** (issue #175, refined by
issue #205). Attempt each tier in order; on success proceed to the PR step.
On failure fall through to the next tier. The workspace `signing_strategy`
field may restrict which tiers to attempt: `github_api` = Tier 1 only,
`local` = Tier 2 only, `manual` = Tier 3 straight away, `auto` (default/empty)
= Tier 1 → Tier 2a → Tier 2b → Tier 3.

**Probe up front: does the base branch enforce signed commits?**

Run once at the start of the commit+push phase:

```
gh api repos/<owner>/<repo>/branches/<base>/protection \
   --jq '.required_signatures.enabled // false' 2>/dev/null
```

`true` → repo enforces signed commits; Tier 2b is forbidden, fall straight to
Tier 3 if Tiers 1 and 2a both fail. `false` or 404 → repo does NOT enforce
signing; Tier 2b (plain unsigned commit) is allowed and is the right fallback
when Tier 2a fails because of a missing/locked GPG key. Cache the answer for
this run so you only probe once.

**Tier 1 — GitHub API commit (signed by GitHub)**

12a. Draft the commit message (subject + body with `Closes #<num>` +
     `Co-Authored-By: PrismConductor worker <noreply@anthropic.com>`).

12a-prep. **Ensure the remote branch exists.** `createCommitOnBranch` requires
     the branch reference to already exist on origin; otherwise the mutation
     returns the input verbatim in `extensions.value` with no clear error
     (issue #205). Check and create as needed:

     ```
     if ! git ls-remote --exit-code --heads origin "$BRANCH" >/dev/null 2>&1; then
       git push -u origin HEAD
     fi
     ```

     Now `expectedHeadOid` (the current local tip SHA) matches the remote
     ref's tip, and the GraphQL mutation can target it.

12b. Call the GitHub GraphQL mutation `createCommitOnBranch` using the
     workspace PAT (`gh api graphql`). The mutation requires:
     - `repositoryNameWithOwner`: `<owner>/<repo>`
     - `branch.branchName`: the current branch name
     - `expectedHeadOid`: `git rev-parse HEAD` (the current tip SHA)
     - `message.headline` / `message.body`: split from the drafted message
     - `fileChanges.additions` / `fileChanges.deletions`: list every modified
       or deleted path with base64-encoded content for additions.

     **Invocation (correct syntax):** build a single JSON file containing both
     `query` and `variables`, then pass it via `--input`:

     ```
     cat > /tmp/gql-payload.json <<'JSON'
     {
       "query": "mutation($input: CreateCommitOnBranchInput!) { createCommitOnBranch(input: $input) { commit { oid url } } }",
       "variables": { "input": { ... full input object ... } }
     }
     JSON
     gh api graphql --input /tmp/gql-payload.json
     ```

     Do **not** use `gh api graphql -f query='...' -f input="$(cat file)"` —
     `-f` only handles string variables, so the input object gets JSON-stringified
     and the server rejects it with the entire input echoed in `extensions.value`.

     **File-size / payload limits:** if the total additions payload exceeds
     ~8 MB or the number of files exceeds 200, skip Tier 1 and fall through
     to Tier 2 — do not attempt a partial commit.

     **Retry on transient errors:** if the API returns a 5xx status, wait 2 s
     and retry once. On second failure fall through to Tier 2a.

     On success: record the returned SHA. The remote already has the commit;
     a local `git pull --ff-only` will fast-forward your worktree to match.
     Proceed to PR creation.

**Tier 2a — local signed commit + push**

12c. `git add` only the files you modified (avoid `git add -A`). If nothing is
     staged and nothing is modified, skip commit and proceed to push.
12d. Write the commit message to `.prismconductor/commit-msg/<issue>.txt` (full
     content; truncate only the GraphQL payload, never the file).
12e. `git commit -S -F .prismconductor/commit-msg/<issue>.txt` with a 30 s
     timeout. Do NOT attempt to supply a passphrase. If the process blocks on
     TTY, times out, OR fails with `cannot run gpg`, `gpg failed to sign`,
     `secret key not available`, or any other signing error, fall through to
     **Tier 2b** (or Tier 3 if the repo enforces signed commits — see probe).
12f. `git push -u origin HEAD`.

**Tier 2b — local unsigned commit + push (only when signing is NOT required)**

12c-2b. Same staging step as 12c (only modified files).
12d-2b. Same commit-msg file as 12d (write the message file even though
        the commit itself is unsigned, so resume / continue paths can find it).
12e-2b. `git commit -F .prismconductor/commit-msg/<issue>.txt` (no `-S`).
        This produces an unsigned commit, which is acceptable when the repo's
        base-branch protection does NOT require signed commits.
12f-2b. `git push -u origin HEAD`. On any push failure (auth, branch protection,
        network), fall through to Tier 3.

If the up-front signed-commits probe returned `true`, **skip Tier 2b entirely**
— a plain commit will be rejected by the remote anyway, and the user genuinely
needs to push manually with their signing key. Go straight from a Tier 2a
failure to Tier 3.

**Tier 3 — preserved worktree + manual command (NEEDS_PR)**

12g. Stage all changes: `git add -A`.
12h. Write the full drafted commit message to
     `.prismconductor/commit-msg/<issue>.txt` (create parent directories if
     missing).
12i. Print the NEEDS_PR sentinel on its own line:
     ```
     NEEDS_PR: commit signing unavailable — manual push required
     ```
     The conductor parses this, preserves the worktree, and surfaces a
     copy-pastable command to the user. Do NOT print `PR_OPENED:` or
     `Work complete.` — those are reserved for successful pushes.

     After printing the sentinel, exit cleanly (exit 0). The conductor moves
     the card to NEEDS_PR state.

**After a successful Tier 1, 2a, or 2b push:**

13. **Single-PR enforcement (#17, Q6):** before `gh pr create`, run `gh pr list --head <branch> --json number,url`. If non-empty, an earlier resume already opened the PR — append a follow-up comment via `gh pr comment <num> --body-file -` summarizing this leg's work, capture the existing URL, and SKIP `gh pr create`. Otherwise: `gh pr create --draft --base <BASE> --title "<short subject> (#<num>)" --body-file -`. The body should contain:
    - A 1-2 sentence summary
    - The list of files modified
    - **Verification:** lint command + result, build command + result, smoke command + result (`pass` / `fail` with first console error / `skipped — spec not present`), test command + result with pass/fail counts
    - Which commit tier succeeded (Tier 1 / Tier 2)
    - Anything skipped or flagged for human review
    - `Workspace mode: per-execute worktree at <pwd>` — surfaces the conductor isolation mode for reviewers (run `pwd` to fill the path).
    - The literal trailer line `Closes #<num>`
14. Capture the PR URL from `gh pr create` (or `gh pr list` in the reuse case).

### 6. Sentinels for the conductor

17. Print exactly `PR_OPENED: <url>` on its own line so the conductor can parse the PR number and move the card to TESTING.
18. Print `Work complete.` so the PTY parser advances state (§10.3).

### Failure paths (NON-NEGOTIABLE: ALWAYS print `BLOCKED:` before exiting)

If you cannot complete this skill for ANY reason — permission denial, command refused, network error, missing file, lint/build/test fail, push rejected, branch conflict, ANYTHING — your last printed line MUST start with the literal `BLOCKED: ` followed by a one-line reason. The conductor only treats a session as truly blocked when it sees that exact prefix; printing `Halting because…` or just exiting cleanly without the sentinel makes the card show "completed" even though no work shipped.

- Steps 4-6 (branch hygiene) failures → `BLOCKED: <reason>`. No branch created.
- Step 9-11 (verification) failures → `BLOCKED: <reason>`. Branch exists with WIP code; do not push.
- Steps 12-15 (commit/push/PR) failures → `BLOCKED: <reason>`. Branch + commit exist locally.
- Tool-permission denial (Bash/Edit/Write refused) → `BLOCKED: tool denied: <command>`. Do not interpret a denial as "user wants me to stop quietly" — surface it.
- Anything else preventing reaching step 17 → `BLOCKED: <reason>`.

`Work complete.` and `PR_OPENED:` are reserved for the success path only — never print them on a failure.

## Resume mode (`--resume-question <id>`)

Issue #17. The previous execute worker invoked `/conductor-question` and exited; the user has now answered, and you've been re-spawned to continue.

- **Skip steps 1-3.** Instead read:
  - `.prismconductor/questions/<id>.context.json` — minimal sidecar (`issue_number`, `workspace_id`, `revision`, `branch`, `plan_path`, `scratch`).
  - `.prismconductor/answers/<id>.json` — the user's answer in `MidRunAnswer` shape: `{question_id, answer, multi}`.
  - The plan referenced by `context.plan_path`.
- **Branch hygiene:** the worktree already exists with the prior worker's edits. `git status --short` MAY be non-empty. Verify `git branch --show-current` matches `context.branch`; if not, print `BLOCKED: branch mismatch — expected <ctx.branch>, got <current>` and exit. Do NOT create a new branch and do NOT discard existing changes.
- **Continue the work** that the prior worker paused on, using the user's answer as guidance. The `scratch` field in the context sidecar is your prior self's note about what to do next.
- The rest of the flow (verification, commit, push, PR) is identical — except the single-PR enforcement at step 15 is the load-bearing rule that keeps a multi-pause issue from opening multiple draft PRs.

## Mid-execution questions

If the agent gets stuck on something the plan didn't cover, invoke `/conductor-question` (writes a structured question, pauses for answer). Do this before staging changes if possible — once edits are queued, you should drive the work to completion or commit a partial WIP first.
