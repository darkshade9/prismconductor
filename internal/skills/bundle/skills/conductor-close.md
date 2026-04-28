---
name: conductor-close
description: Universal `/check-and-close` analogue. Posts a completion summary to the GitHub issue and closes it. No commit/push beyond what was already done.
---

# conductor-close

Bundled by PrismConductor (PRISMCONDUCTOR_PLAN.md §15.7).

## Inputs

- `--issue <number>` (required)
- `--pr <number>` (optional — links the closing PR)

## Behavior

1. Compose a brief completion comment: what was done, the PR link, any follow-ups noted during execute.
2. Post via `gh issue comment <issue> --body <markdown>`.
3. Close via `gh issue close <issue>`.
4. Print `Work complete.`.

Does not commit or push. Execute mode already did that.
