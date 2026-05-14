# Data Contracts

Schemas in this document are contracts between skills, backend, persistence, and UI. Update this document when changing fields, parser sentinels, or compatibility rules.

## Plan JSON

Plan files are written under:

```text
.prismconductor/plans/<issue>-rev<N>.json
```

Required fields for new plans:

```json
{
  "issue_number": 123,
  "workspace_id": "workspace-id",
  "revision": 1,
  "goal_summary": "What the issue is trying to accomplish.",
  "executive_summary": "Plain-language outcome summary.",
  "plan_markdown": "Technical implementation plan.",
  "files_to_modify": [
    {"path": "internal/example.go", "intent": "modify"}
  ],
  "dependencies_detected": [],
  "cross_deps_detected": [],
  "suggested_labels": [],
  "questions": [],
  "estimated_complexity": "M",
  "ready_to_execute": true
}
```

Notes:

- `issue_number`, `revision`, `plan_markdown`, `files_to_modify`, `dependencies_detected`, `questions`, `estimated_complexity`, and `ready_to_execute` are validated by `internal/planio`.
- `goal_summary` and `executive_summary` are `omitempty` in Go for backward compatibility, but bundled skills require them for new plans.
- Unknown extra fields are tolerated so the schema can grow.
- Aliases such as `issue` or `rev` are rejected.
- A plan must not set `ready_to_execute: true` when the issue body could not be fetched.

## File Intent

```json
{"path": "relative/path", "intent": "add"}
```

Supported intents are conventionally:

- `add`
- `modify`
- `delete`

Plan and execute skills should be explicit. UI and validators may grow stricter over time.

## Question JSON

Questions appear inside plan JSON and mid-run question files.

```json
{
  "id": "stable-question-id",
  "type": "single_choice",
  "prompt": "Which storage behavior should be used?",
  "options": ["Use SQLite.", "Use repo-local JSON."],
  "default": "Use SQLite.",
  "required": true,
  "audience": "user"
}
```

Supported `type` values:

- `single_choice`
- `multi_choice`
- `free_text`
- `yes_no`

Rules:

- `prompt` is only the question, not inline answer choices.
- `options` contains full option text for choice questions.
- `yes_no` questions omit `options`.
- `default` should be exactly one recommended answer.
- `audience` is optional; empty or `user` surfaces to the human, `peer_agent` routes to an architect worker.

Mid-run questions are written under:

```text
.prismconductor/questions/<id>.json
```

Answers are written under:

```text
.prismconductor/answers/<id>.json
```

## Session Sentinels

Session transcript parsing uses plain string contains, not regex. Constants live in `internal/session/patterns.go`.

| Sentinel | Meaning |
|---|---|
| `Question: ` | Plan-mode question path. |
| `Plan written to .prismconductor/plans/` | Plan file is ready. |
| `Work complete.` | Worker completed successfully. |
| `BLOCKED:` | Worker cannot continue without intervention. |
| `PR_OPENED: ` | Execute worker opened a PR. |
| `QUESTION_PENDING: ` | Mid-run question was written and session should pause. |
| `NEEDS_PR:` | Work succeeded but push/PR creation needs manual recovery. |
| `FANOUT_WRITTEN: ` | Fanout analysis JSON was written. |

Do not change these strings casually. They are part of the worker/backend contract.

## Skill References

Bundled skill refs use:

```text
bundled:<name>
```

Repo skill refs use absolute file paths.

Workspace skill profiles map stages to skill refs:

- `plan`
- `execute`
- `continue`
- `close`

Missing stage entries fall back to legacy mode behavior.

## Board Columns

Current board column constants:

- `todo`
- `plan`
- `in_progress`
- `blocked`
- `review`
- `done`

Treat column names as persisted UI/backend state.
