---
name: conductor-question
description: Helper invoked inline when a worker needs to ask the user a structured question mid-execution. Writes the question to .prismconductor/questions/<id>.json and prints a sentinel for the conductor to detect.
---

# conductor-question

Bundled by PrismConductor (PRISMCONDUCTOR_PLAN.md §15.7, issue #17).

## Inputs

- `--type <single_choice|multi_choice|free_text|yes_no>`
- `--prompt <text>`
- `--options <comma-separated>` (for choice types)
- `--required <true|false>`
- `--default <value>` (REQUIRED — exactly one recommended answer, same per-type rules as `conductor-plan`'s questions: full option text for choice types, `"yes"` or `"no"` for yes_no, a one-or-two-sentence sample answer for free_text)

## Behavior

The execute worker runs in `claude -p` (one-shot) mode and cannot block on user input. Mid-run questions are persisted to disk; the conductor pauses the session, surfaces the question to the user, and re-spawns a fresh execute worker on the same branch once the answer arrives. This skill writes the question + sidecar and exits.

1. Generate a UUID `<id>` for the question.
2. Write `.prismconductor/questions/<id>.json` matching the §6.4 Question schema:
   ```json
   {
     "id": "<id>",
     "type": "<type>",
     "prompt": "<prompt>",
     "options": ["<opt1>", "..."],
     "default": "<default>",
     "required": true
   }
   ```
3. Write `.prismconductor/questions/<id>.context.json` with the minimal sidecar the resume worker needs:
   ```json
   {
     "issue_number": <num>,
     "workspace_id": "<wsID>",
     "revision": <N>,
     "branch": "<feat/issue-<num>-<slug>>",
     "plan_path": ".prismconductor/plans/<num>-rev<N>.json",
     "scratch": "<free-text notes for your future self — what you were doing, why you're stuck>"
   }
   ```
4. Print exactly `QUESTION_PENDING: <id>` on its own line. The conductor's PTY parser flips the session state to `paused_for_question` (NOT `failed`).
5. Exit 0.

**Do NOT** poll for the answer file (the parent worker is `claude -p` and exits with you). **Do NOT** print `BLOCKED:` (this is not a failure — it's a pause). **Do NOT** print `Question:` (that sentinel is reserved for plan-mode and would double-fire the wrong UI path).

After the user answers via the conductor UI, a fresh execute worker is spawned with `/conductor-execute --resume-question <id>`. That worker reads the context sidecar, switches back to the same branch, and continues.
