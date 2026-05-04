# PrismConductor

A goal-driven, multi-workspace agent orchestration desktop app. Plan, execute, review, and ship GitHub issues across one or many repos with a kanban board that tracks Claude / OpenAI / Gemini / Ollama agents on your behalf.

[![build](https://github.com/darkshade9/prismconductor/actions/workflows/build.yml/badge.svg?branch=main)](https://github.com/darkshade9/prismconductor/actions/workflows/build.yml)
[![smoke](https://github.com/darkshade9/prismconductor/actions/workflows/smoke.yml/badge.svg?branch=main)](https://github.com/darkshade9/prismconductor/actions/workflows/smoke.yml)

> **Status: pre-1.0, actively under development.** Daily-driver-usable for the maintainer; some features in flight (remote workspaces, agent-terminal panel, three-tier signed-commit fallback). See [open issues](https://github.com/darkshade9/prismconductor/issues) for the roadmap.

See [`PRISMCONDUCTOR_PLAN.md`](PRISMCONDUCTOR_PLAN.md) for the full design spec.

## Capabilities

PrismConductor turns GitHub issues into a kanban board where AI agents do the bulk of the engineering work, and you only step in for plan review and final approval.

**Core flow**

- **Multi-workspace board.** Aggregate issues across many repos into one TODO / PLAN / IN_PROGRESS / REVIEW / DONE pipeline. Filter by workspace chip, by active goal, by labels (AND / OR), by title search.
- **Goal-driven backlog ranking.** Define a Goal scoping which issues are in play; the orchestrator ranks the backlog by dependency-awareness so you always work the right thing next.
- **Auto-pull from TODO.** Workers spawn into PLAN automatically when a slot frees and an unblocked top-of-backlog issue is waiting.
- **Plan / execute / close pipeline.** Each card moves through a configurable pipeline of skills: `conductor-plan` → `conductor-execute` → `conductor-close`, with optional adversarial-review and conflict-resolver steps.
- **Per-pool worker pools.** Configure many LLM providers / models / capacities per workspace. Plan, work, and orchestrator roles route to dedicated pools. Drag-to-reorder preference, per-pool budgets, per-pool temperature overrides.
- **Mid-run questions.** A worker that needs clarification mid-execute pauses and asks the user via a structured question form; answer flows back to the worker and resumes.
- **Continue Work.** Re-engage on a REVIEW-column PR with reviewer feedback or a free-form note; the worker picks up on the same branch with the same context.
- **Plan re-ingest.** Plans live in `.prismconductor/plans/<n>-rev<N>.json` so revisions are persistent and re-spawnable.

**Reliability and recovery**

- **Worktree preservation on failure** so a worker that completed substantial work but couldn't commit/push (GPG signing failed, branch protection rejected, etc.) leaves its diff on disk for manual recovery — the conductor never auto-deletes user-paid-for work.
- **Atomic duplicate-spawn guard** prevents two concurrent workers from running on the same `(workspace, issue, mode)` and double-billing.
- **Self-healing pool counters** reconcile against DB ground truth on every terminal session event so slot-leak ghosts ("Workers 1/2 with no actual worker") can't accumulate.
- **Stale-state reconcilers** at startup clean up any session rows left in `running` after a crash, any `waiting_for_pool` flags pinned across PR-merge, any orphan paused-for-question sessions.
- **Re-attach across restarts.** Live PIDs at shutdown are picked back up at next startup; transcripts seamlessly continue.

**GitHub integration**

- **5-minute poll** of every workspace's open issues + PR state. Auto-detect: PR opened, PR merged, PR closed unmerged, checks failed, checks recovered, merge conflicts (with the precise conflicting file list via `git merge-tree`, not the full PR changeset), comments added.
- **Auto-heal on test failure** (`#116`) — when CI fails on a REVIEW PR, the conductor can auto-spawn a self-heal session up to N times.
- **Resolve Conflicts button** (`#124`) — surface a card-level red badge with the actually-conflicting file list and one-click rebase + merge worker.
- **Auto-cancel zombie workers** (`#113`, `#118`) when a PR merges/closes/opens, so workers don't keep burning tokens after the deliverable is shipped.
- **Bundled mode** (`#15.8` of the plan) — a bare repo with no `.claude/` or `CLAUDE.md` works on day one. No setup tax for new repos.

**Cost and observability**

- Per-session token + cost tracking, surfaced on each card with a tooltip breakdown.
- Per-pool / per-workspace / per-goal spend rollups, today and this week.
- Live activity strip on every running card showing the agent's most recent tool calls and a Stop button.

**In flight (see open issues)**

- **Remote workspaces** (`#171`) — execute on Cloudflare Workers instead of your laptop, so big runs aren't gated by your local CPU. Requires #177 (worker auth, security blocker) before merge.
- **Agent terminal panel** (`#161`) — embedded PTY for chatting directly with `claude` / `aider` / `gemini` from inside the conductor.
- **Three-tier signed-commit fallback** (`#175`) — GitHub-API commits → local signing → NEEDS_PR with prepared push command.
- **Theming + customizable glow colors** (`#142`).
- **PR comment UX inside the conductor** (`#159`) — full GitHub-stand-in for review feedback.

## Test coverage

- **Backend (Go):** ~25 packages. Unit + integration tests covering the session manager, pool registry, store migrations, GitHub poller, IssueView assembler, orchestrator, harness loop, conflict detection, etc. CI runs `go test ./...` on every push.
- **Frontend (TS):** typecheck via `tsc --noEmit` in CI; component tests via Vitest.
- **Smoke (E2E):** Playwright boots the app under `xvfb-run`, asserts React mounts and there are no console errors. Runs on every push to `feat/issue-*` and `main`.
- **Multi-platform build matrix:** `build.yml` builds the Wails binary on Linux, macOS, and Windows. Linux additionally runs the Playwright smoke against the headless app.
- **Security:** `gosec`, `govulncheck`, `npm audit`, `Trivy`, and `gitleaks` in the `security` job. High-severity findings fail the build.

## Install (end-user)

> Pre-built binaries are not yet published. Build from source using the steps below until v1.0.

## Build from source

### Prerequisites

| Tool | Notes |
|--|--|
| Go ≥ 1.25 | `brew install go` (macOS) / your distro's package manager |
| Node ≥ 20 | `brew install node` |
| Wails v2 | `go install github.com/wailsapp/wails/v2/cmd/wails@latest` |
| `gh` CLI | Required at runtime for GitHub API calls. Authenticate with `gh auth login` |
| Git ≥ 2.38 | `git merge-tree --name-only` flags require this version |
| Platform deps |  See per-platform notes below |

**macOS:** `brew install pkg-config`

**Linux:** `sudo apt-get install libgtk-3-dev libwebkit2gtk-4.0-dev build-essential pkg-config`

**Windows:** `choco install mingw`

### Build

```bash
git clone https://github.com/darkshade9/prismconductor.git
cd prismconductor
wails build
```

Produces `build/bin/prismconductor.app` (macOS), `build/bin/prismconductor` (Linux), or `build/bin/prismconductor.exe` (Windows).

For development with hot-reload:

```bash
wails dev
```

For a release-style build that wipes and rebuilds the app bundle (handy when frontend caches go stale):

```bash
./scripts/restart.sh             # release build
./scripts/restart.sh --debug     # debug build with DevTools enabled (Cmd+Opt+I)
./scripts/restart.sh --no-pull   # offline build
```

### Run tests

```bash
go test ./...                          # backend unit + integration
cd frontend && npx tsc --noEmit        # frontend typecheck
cd tests && npm install && npm run smoke:install && npm run smoke   # Playwright smoke
```

## Setup

On first launch:

1. **Add a workspace.** Click `+ Add Workspace`. Provide a name, a local repo path (must be a clone of a GitHub repo), the GitHub owner, the GitHub repo, and a workspace color. The conductor's `gh` CLI auth handles GitHub access; you don't paste tokens for local workspaces.

2. **Add at least one work pool.** Settings → Pools → `+ Add Pool`. Provide:
   - Name (free-form, e.g. `claude-sonnet-4-6`)
   - Provider (`Claude`, `OpenAI`, `Gemini`, `Ollama`, `LMStudio`, `LiteLLM`)
   - Model (auto-listed on `Test connection`)
   - Endpoint + API key (provider-specific)
   - Capacity (how many concurrent workers this pool can run)
   - Role (`work`, `plan`, or `orchestrator`)

   Drag rows to reorder preference. Multiple pools per role are supported; the orchestrator routes by priority + capacity.

3. **(Optional) Set a goal.** Click `+ New Goal` in the top bar. Give it a one-line title; the orchestrator will scope backlog ranking to issues that look related.

4. **Wait for the GitHub poll** (or click Refresh in the workspace switcher). Open issues from the configured repo populate the TODO column.

## Use

### Plan an issue

Drag an issue from TODO into PLAN — or wait for auto-pull to drag it for you when a planner slot frees. The plan worker spawns; the card glows blue while it runs. When the plan is ready, the card glows amber and a `(rev N) ready` badge appears.

Click the card to open the Plan modal. Review the proposed file changes, the structured questions, and the cost estimate. Either:

- **Approve** to spawn the execute worker on a fresh feature branch
- **Refine** with a free-form note and re-spawn the planner
- **Reject** to discard the plan and move the card back

### Execute and review

The execute worker writes code, runs lints/tests, commits, pushes, and opens a PR. Card moves to REVIEW with a green PR chip. The activity strip shows live tool calls.

If checks fail or merge conflicts appear, the conductor surfaces a red banner with one-click recovery (`Self-heal`, `Resolve Conflicts`, `Continue Work`).

### Merge

Merge the PR on GitHub. The conductor's poller detects the merge within ~5 minutes and moves the card to DONE. (In-app merge button is on the roadmap; see #146.)

### Stop / cancel

Every running card has a Stop button on its activity strip. Stops the worker, releases the slot. The session is marked `failed` with reason `cancelled by user`; the worktree is preserved so you can pick up the work manually if needed.

## Layout

```
internal/
  agentterm/        # embedded PTY agent panel
  archiver/         # auto-archive DONE cards by age
  eventbus/         # in-process event bus
  git/              # worktree + merge-tree helpers
  github/           # poller + REST/GraphQL client
  harness/          # in-process LLM agent loop (non-Claude providers)
  issueview/        # canonical IssueView assembler
  llm/              # provider-agnostic LLM client interface
  orchestrator/     # backlog ranking + auto-pull
  pipeline/         # configurable per-workspace step pipeline
  remoteworker/     # Cloudflare Workers execution backend (in flight)
  secretstore/      # OS-keychain wrapper
  session/          # session manager (subprocess + harness paths)
  skills/bundle/    # bundled conductor-* skill markdown
  store/            # SQLite persistence + migrations
  workerpool/       # role-keyed pool registry
frontend/src/
  components/       # Board, Card, PlanModal, Settings, etc.
  stores/           # zustand stores (workspace, issueview, session)
worker/             # Cloudflare Worker bundle (in flight, #171)
scripts/restart.sh  # one-shot rebuild + relaunch
```

## Hard rules (also see [`CLAUDE.md`](CLAUDE.md))

- **Schemas are contracts.** §9.1 (plan JSON) and §6.4 (Question shape) are wire formats between worker, orchestrator, and UI. Don't change a field without updating all three.
- **No polling inside the orchestrator.** The 5-min GitHub fetch is the only polling loop. Everything else is event-driven.
- **Bundled mode is the default.** Bare repos work on day one; we never block on missing repo enrichment.
- **One active goal at a time.** Enforced via SQL transaction in `Store.SetGoalActive`.

## Contributing

Open issues are tracked at [github.com/darkshade9/prismconductor/issues](https://github.com/darkshade9/prismconductor/issues). The conductor itself is dogfooded on its own development — many of those issues are filed by the conductor running on this repo.

PRs welcome; please ensure `go test ./...` and `npx tsc --noEmit` pass.

## License

Not yet licensed for public redistribution. See repository owner for details.
