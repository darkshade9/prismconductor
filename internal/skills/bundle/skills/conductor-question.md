---
name: conductor-question
description: Helper invoked inline when a worker needs to ask the user a structured question mid-execution. Writes the question to .prismconductor/questions/<id>.json and prints a sentinel for the conductor to detect.
---

# conductor-question

Bundled by PrismConductor (PRISMCONDUCTOR_PLAN.md §15.7).

## Inputs

- `--type <single_choice|multi_choice|free_text|yes_no>`
- `--prompt <text>`
- `--options <comma-separated>` (for choice types)
- `--required <true|false>`
- `--default <value>` (REQUIRED — exactly one recommended answer, same per-type rules as `conductor-plan`'s questions: full option text for choice types, `"yes"` or `"no"` for yes_no, a one-or-two-sentence sample answer for free_text)

## Behavior

1. Generate a UUID for the question id.
2. Write `.prismconductor/questions/<id>.json` matching the §6.4 Question schema. Populate `default` from the `--default` input.
3. Print `Question: <prompt>` so the PTY parser flips state to waiting_for_input (§10.3).
4. Block on the matching answer file `.prismconductor/answers/<id>.json` (poll every 2s).
5. Read the answer and return it on stdout for the calling skill.
