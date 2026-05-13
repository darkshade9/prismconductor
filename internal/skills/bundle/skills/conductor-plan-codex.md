---
name: conductor-plan-codex
description: Codex-adapted planning skill. Reads a GitHub issue, analyzes the codebase, and emits a structured plan JSON. Designed for the codex CLI subprocess provider which uses its own built-in agent loop rather than Claude Code's slash-command skill system.
---

# conductor-plan-codex

Bundled by PrismConductor. Used by codex-provider pools (PRISMCONDUCTOR_PLAN.md §15.7, issue #281).

> NOTE (issue #281): This skill is the codex-adapted variant of `conductor-plan`. It is
> functionally identical in output contract — same plan JSON schema, same sentinels — but
> written as a self-contained task description suitable for an autonomous agent CLI that
> does not have Claude Code's slash-command skill system. Use this as the workspace plan
> skill when the pool's provider is `codex`.

## Task

You are an autonomous software planning agent. Your job is to read a GitHub issue, understand the codebase it references, and produce a structured plan JSON that a downstream execute worker can implement.

## Inputs

- `--issue <number>` (required)
- `--repo <path>` (defaults to cwd)
- `--related-repos <path>` (repeatable; absolute paths to sibling repositories you may grep / read for context)

Parse these from the arguments you were invoked with.

## Sibling repos (read-only)

If `--related-repos` is set: the listed paths are SIBLING repositories you may grep / read for context. **Do NOT modify any file under those paths.**

## Behavior

1. Fetch the issue body:
   ```
   gh issue view <number> --json title,body,labels
   ```
   If this fails, print `BLOCKED: cannot fetch issue #<number>: <error>` and stop.

2. Read `CLAUDE.md` (if present at `<repo>/CLAUDE.md`) and any files in `<repo>/.claude/rules/`.

3. Explore the repository structure: list top-level directories, read key files relevant to the issue, grep for symbols, types, and patterns that the issue references.

4. Synthesize a plan. The plan must:
   - Identify every file that must be created or modified (with explicit `"intent": "add"` or `"intent": "modify"`).
   - List `dependencies_detected` (issue numbers this depends on, if any are referenced in the body).
   - Include `suggested_labels` (array of strings).
   - Set `estimated_complexity` to one of the values defined in the workspace complexity scale (S, M, L, XL, or as shown in the prompt prefix).
   - Set `ready_to_execute` to `true` only when all questions have defaults or are answered.
   - Set `goal_summary` (1-2 sentences) and `executive_summary` (2-3 sentences) and `plan_markdown` (full Markdown description of the approach).
   - If clarifying questions are needed before implementation can begin, add them to the `questions` array. Each question must have `id`, `type` (`single_choice` or `yes_no`), `prompt`, `options` (for single_choice), `default`, and `required` fields.

5. Write the plan JSON to `.prismconductor/plans/<issue>-rev<N>.json` where `<N>` is the next revision number (1 for a new issue, previous+1 for re-plans). The schema:

```json
{
  "issue_number": <number>,
  "revision": <N>,
  "goal_summary": "...",
  "executive_summary": "...",
  "plan_markdown": "...",
  "files_to_modify": [
    {"path": "...", "intent": "add|modify"}
  ],
  "dependencies_detected": [],
  "suggested_labels": [],
  "questions": [],
  "estimated_complexity": "M",
  "ready_to_execute": true
}
```

6. After writing the file, print the sentinel on its own line:
   ```
   Plan written to .prismconductor/plans/<issue>-rev<N>.json
   ```
   This is mandatory — the conductor parses this line to advance the card state.

7. Print `Work complete.` on its own line.

## Failure

If anything blocks you from completing a valid plan:
- Print `BLOCKED: <one-line reason>` on its own line and stop.
- Do NOT write a partial plan file.
