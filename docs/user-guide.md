# User Guide

PrismConductor is a local desktop app for turning GitHub issues into agent-driven engineering work. It is built for a human owner who still controls priority, plan approval, and merge decisions.

The app is useful to two common audiences:

- **Engineers** use it to plan issues, spawn implementation workers, recover failed runs, and open PRs without manually shepherding every agent session.
- **PMs and engineering leads** use it to organize goals, rank issue backlogs, review implementation plans, answer clarifying questions, and track work through review.

## Mental Model

PrismConductor has four core concepts.

| Concept | Meaning |
|---|---|
| Workspace | One local git checkout connected to a GitHub repository. |
| Issue card | A GitHub issue shown on the board. GitHub remains the canonical issue tracker. |
| Goal | A human-written objective used to scope backlog ranking and focus. Only one goal is active at a time. |
| Pool | A configured provider/model/capacity group used to run plan, work, or orchestrator jobs. |

The board columns represent work state:

| Column | Meaning |
|---|---|
| TODO | Issue exists and has not been planned. |
| PLAN | A planner is running, a plan is ready for review, or the issue needs re-planning. |
| IN_PROGRESS | An execution worker is implementing an approved plan. |
| REVIEW | A PR exists or work needs human review/recovery. |
| DONE | The linked issue/PR is complete. |

## Safety Model

PrismConductor can spend money, edit code, push branches, and open PRs under your credentials. Treat it like a junior engineer with shell access:

- Review plans before approving execution.
- Review PRs before merging.
- Start with low pool capacity until you understand cost and behavior.
- Prefer local workspaces for sensitive code.
- Keep provider API keys and GitHub auth scoped to the repositories you intend to use.

Failed or blocked execute runs preserve worktrees where possible so paid-for work is not silently deleted.

## Setup

### 1. Install Runtime Tools

You need:

| Tool | Why |
|---|---|
| Git | Workspace checkout, branches, worktrees, and merges. |
| GitHub CLI (`gh`) | Issue/PR reads and writes. Run `gh auth login` first. |
| At least one agent/provider | Claude CLI, Codex CLI, OpenAI-compatible endpoint, Gemini, Ollama, LM Studio, or LiteLLM. |

If building from source, also install Go, Node, and Wails. See [Contributing Guide](contributing.md#local-development-setup).

### 2. Add A Workspace

Open Settings > Workspaces and add a local repository.

Provide:

- display name
- local repo path
- GitHub owner
- GitHub repo
- default branch if it is not detected
- color

The repo does not need any agent-specific files. Bare repositories work through bundled skills. Repositories can improve output quality over time by adding `AGENTS.md`, `CLAUDE.md`, `.codex/rules/`, `.claude/rules/`, or repo-local skills.

Remote workspaces are currently paused in the local spawn paths. Existing remote workspace rows are preserved, but new day-to-day usage should use local workspaces.

## Providers And Pools

Providers store credentials and endpoints. Pools decide which provider/model/capacity is used for each kind of work.

### Configure Providers

Open Settings > Providers.

A provider entity stores reusable credentials and endpoint information for a driver kind. Provider support is intentionally heterogeneous:

| Provider | Typical Use |
|---|---|
| Claude | Local Claude Code subprocess and Anthropic-compatible planning/orchestration. |
| Codex | Local Codex CLI subprocess. |
| OpenAI | Hosted OpenAI models through API credentials. |
| Gemini | Google Gemini models. |
| Ollama | Local OpenAI-compatible models. |
| LM Studio | Local OpenAI-compatible server. |
| LiteLLM | Proxy/router for multiple model backends. |

Some providers require API keys; local CLI/subscription providers may rely on their own login state instead.

### Configure Pools

Open Settings > Pools and create at least:

- one **plan** pool
- one **work** pool

Optional:

- one **orchestrator** pool for goal/backlog ranking

Pool fields:

| Field | Meaning |
|---|---|
| Provider | Driver kind or saved provider entity. |
| Model | Model ID. Some providers can list models; others accept free text. |
| Role | `plan`, `work`, or `orchestrator`. |
| Capacity | Maximum concurrent sessions for that pool. Start with `1`. |
| Scope | Global or workspace-specific. |
| Priority | Lower/dragged-higher rows are preferred; ties round-robin. |
| Budget/rates | Used for cost display and projections. |

If no plan pool is configured, affected auto-pull flows are paused.

## Daily Workflow

### 1. Create Or Select A Goal

Use the Goal pane to create a goal with:

- title
- intent
- acceptance notes
- optional labels, milestone, or free-text filter

Activate one goal at a time. The active goal helps filter and rank the backlog. PMs usually live here: define intent, inspect candidate issues, and decide what is in scope.

### 2. Pull Issues Into TODO

PrismConductor polls GitHub periodically and imports open issues for each enabled workspace. You can refresh manually from the workspace controls.

GitHub remains canonical. Editing issue titles, labels, or descriptions happens in GitHub unless a specific in-app control exists.

### 3. Plan An Issue

Drag a TODO card to PLAN or let auto-pull start planning when a plan pool slot is free.

The planner:

- reads the issue
- reads repo instructions when present
- searches relevant code
- writes `.prismconductor/plans/<issue>-rev<N>.json`
- suggests labels
- asks structured questions when needed

Open the card and review:

- goal summary
- executive summary
- technical plan
- files to modify
- questions and recommended defaults
- cost estimate, when rates are configured

Choose:

- **Approve** to execute.
- **Refine** to add guidance and re-plan.
- **Reject** to discard the plan.

### 4. Execute Approved Work

After approval, PrismConductor creates a managed worktree and feature branch. The execute worker implements the plan, runs verification, commits, pushes, and opens a draft PR when possible.

The card shows live activity and provider/pool information. You can stop a running session from the card.

### 5. Answer Questions

If an execute worker hits an ambiguity, it can pause with a structured question. Answer it in the app. The worker resumes on the same branch with the answer context.

### 6. Review And Recover

In REVIEW, inspect the PR on GitHub and use PrismConductor for recovery actions:

| Situation | Action |
|---|---|
| Reviewer wants changes | Use Continue Work with a note. |
| CI failed | Use Self-heal when available, or Continue Work with details. |
| Merge conflict | Use Resolve Conflicts when the conflict badge appears. |
| Could not push/open PR | Inspect the preserved worktree and follow the NEEDS_PR guidance. |

### 7. Merge And Close

Merge the PR in GitHub. PrismConductor detects the change on the next poll and moves the card toward DONE. If a close skill is configured in a pipeline, it can post completion notes and close the issue.

## Skills And Repo Instructions

Bundled skills are the default. They are agent-neutral workflow contracts such as:

- `conductor-plan`
- `conductor-execute`
- `conductor-continue`
- `conductor-close`
- `conductor-question`
- `conductor-resolve-conflicts`

Provider-specific details are handled by the runner layer. The same canonical skill is used whether the work runs through a CLI subprocess or the in-process tool-calling harness.

PrismConductor reads repo instructions when present:

- `AGENTS.md`
- `CLAUDE.md`
- `.codex/rules/*.md`
- `.claude/rules/*.md`

For best results, keep repo instructions short, concrete, and focused on commands, architecture, conventions, and known pitfalls.

## PM Workflow

PMs and leads usually focus on:

1. Keep GitHub issues clear and acceptance-oriented.
2. Create a goal that explains the current product outcome.
3. Let the planner produce implementation options.
4. Review executive summaries and questions.
5. Approve execution only when the plan matches the intended outcome.
6. Review PR summaries and decide when to merge.

You do not need to read every code-level detail, but you should inspect questions and make sure the plan’s user-visible behavior is correct.

## Engineer Workflow

Engineers usually focus on:

1. Configure pools and provider credentials.
2. Add or maintain repo instructions.
3. Review technical plans before approval.
4. Inspect failed worktrees and PR diffs.
5. Use Continue Work for targeted fixes.
6. Improve bundled or repo-local skills when repeated failure patterns appear.

Before merging, treat agent PRs like human PRs: run or trust CI, inspect diffs, and check that tests cover new behavior.

## Troubleshooting

| Symptom | Check |
|---|---|
| No cards appear | Workspace enabled, GitHub owner/repo correct, `gh auth status` passes. |
| Planning never starts | A plan pool exists, is enabled, has capacity, and the workspace is not pinned to a disabled pool. |
| Worker waits for pool | Increase capacity or wait for active sessions to finish. |
| Provider test fails | Endpoint/API key/login state and selected model. |
| Cost is blank | Add model rates in pool settings, or use a provider marked as free/local. |
| Remote workspace says paused | Convert it to local in Settings; remote execution is paused. |
| Diagnostics needed | Settings > Advanced > Show Diagnostics tab, or use the diagnostics chord documented in `AGENTS.md`. |

## Costs

See [Cost Expectations](cost-expectations.md). Start with capacity `1` and lower-cost planning models until you know the workload shape.
