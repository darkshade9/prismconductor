---
name: conductor-plan
description: Universal `/start-issue` analogue. Reads a GitHub issue, greps the repo, reads any CLAUDE.md and .claude/rules/ present, and emits a structured plan JSON to .prismconductor/plans/<issue>-rev<N>.json. Stops at the proposal gate. No code mutation.
---

# conductor-plan

Bundled by PrismConductor. Used in Bundled and Hybrid skill modes (PRISMCONDUCTOR_PLAN.md §15.7).

## Inputs

- `--issue <number>` (required)
- `--repo <path>` (defaults to cwd)
- `--native-cmd <command>` (Hybrid mode: wraps a repo's own planning skill)

## Behavior

1. Fetch the issue body via `gh issue view <number> --json title,body,labels`.
2. Read `CLAUDE.md` and `.claude/rules/*.md` if present (silently skip if absent — see §15.8).
3. Grep the repo for terms in the issue title and body that name files/symbols.
4. Run `gh label list -R <owner>/<repo> --json name,color,description --limit 200` to see the repo's
   label vocabulary. From the issue title + body + the file paths picked above, suggest the labels
   whose name or description plausibly applies to this issue. Err on the side of restraint
   (max 5 suggestions). Never invent labels that don't already exist on GitHub.
5. Compose a plan that lists:
   - What to do (markdown)
   - Files to add/modify/delete
   - Detected dependencies (other open issue numbers)
   - Suggested labels (from step 4; empty array if nothing matches)
   - Open questions to ask the user (structured per §6.4 Question schema)
6. Write the plan to `.prismconductor/plans/<issue>-rev1.json` matching §9.1 exactly. Include the
   suggestions as the `suggested_labels` array (omit the field entirely if empty).
7. Print `Plan written to .prismconductor/plans/<issue>-rev1.json` so the conductor's PTY parser picks it up (§10.3).
8. Stop. Do not edit code.

## Revisions

If `.prismconductor/answers/<issue>-rev<N>.json` exists, regenerate as rev<N+1> consuming the answers.
