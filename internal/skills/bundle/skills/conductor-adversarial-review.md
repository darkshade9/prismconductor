---
name: conductor-adversarial-review
description: Adversarial code reviewer that challenges the execute worker's output and emits structured findings with PASS or FAIL sentinels for the pipeline driver.
---

# conductor-adversarial-review

Bundled by PrismConductor (PRISMCONDUCTOR_PLAN.md §15.7, issue #146).

This is a **pipeline step skill** designed to run after `conductor-execute` as part of a
configurable workspace pipeline. It reads the execute worker's PR and transcript, challenges
the implementation adversarially, and emits structured findings plus a `PASS` or `FAIL`
sentinel. The pipeline driver uses the sentinel to route to the next step.

## Inputs

- `--issue <number>` (required)
- `--repo <path>` (defaults to cwd)
- `--step-context <path>` (optional — path to .prismconductor/steps/<card-run>/ for prior step transcripts)

## Behavior

### 1. Locate the PR

1. Read `.prismconductor/plans/<issue>-rev<N>.json` (latest approved plan) for context.
2. Find the open PR for this issue: `gh pr list --search "Closes #<issue>" --state open --json number,url,headRefName`.
3. If no open PR exists, print `BLOCKED: no open PR found for issue #<issue>` and exit.

### 2. Adversarial review

Perform a skeptical, adversarial review of the PR diff:

```
gh pr diff <pr_number>
```

Examine the diff against the plan with the following lenses:

- **Correctness**: Does the implementation match the plan's acceptance criteria?
- **Edge cases**: Are there inputs or states the implementation mishandles?
- **Security**: Does any change introduce injection, auth bypass, or data exposure?
- **Tests**: Are new behaviors actually exercised by tests, or do tests pass vacuously?
- **Regressions**: Does any change break adjacent behavior not covered by the plan?
- **Scope creep**: Are there unplanned changes that introduce risk?

Be adversarial — assume the implementation is wrong until proven correct.

### 3. Emit structured findings

Print findings as a fenced JSON block so the conductor can parse them:

```json
{
  "findings": [
    {
      "severity": "critical|major|minor|info",
      "file": "path/to/file.go",
      "line": 42,
      "description": "one-line description of the finding"
    }
  ],
  "summary": "overall verdict — one paragraph"
}
```

Severity scale:
- `critical` — blocks merge; could cause data loss, security breach, or production crash
- `major` — should be fixed before merge; functional defect or test gap
- `minor` — should be addressed but low risk; style, naming, or minor logic smell
- `info` — observation only; no action required

### 4. Emit sentinel

After the findings block, print one of:

- `PASS` — no critical or major findings; the implementation is acceptable
- `FAIL` — one or more critical or major findings; route back to Execute

The pipeline driver routes the card based on this sentinel:
- `PASS` → advance to the next configured pipeline step (e.g. Close)
- `FAIL` → route to `on_fail` (e.g. loop back to Execute) up to `max_loops` times

### 5. Write step output

Write the findings JSON to `.prismconductor/steps/<issue>/adversarial-review.json` so
subsequent pipeline steps (e.g. a second Execute run) can read the defect list.

```bash
mkdir -p .prismconductor/steps/<issue>
cp <findings-file> .prismconductor/steps/<issue>/adversarial-review.json
```

### Failure paths

- No open PR → `BLOCKED: no open PR found for issue #<issue>`
- `gh pr diff` fails → `BLOCKED: could not fetch PR diff — <error>`
- Cannot write step output → proceed anyway; log a warning

Print `Work complete.` after the sentinel.
