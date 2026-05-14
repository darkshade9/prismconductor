# Agent-Neutral Skills

PrismConductor skills are canonical workflow contracts. They should work across any supported agent runtime without maintaining provider-specific copies.

## Core Rule

Author one skill per workflow.

Do:

- `conductor-plan`
- `conductor-execute`
- `conductor-continue`
- `conductor-close`
- `conductor-question`

Do not create provider forks:

- `conductor-plan-codex`
- `conductor-plan-claude`
- `conductor-execute-openai`
- `conductor-execute-gemini`

Provider differences belong in runner adapters, not in duplicated skill markdown.

## Why

Provider-specific skill files drift. Drift creates bugs where one agent learns a newer schema or sentinel and another agent keeps using the old one. PrismConductor’s product promise is vendor neutrality, so skill contracts must be maintained once.

## Runtime Adaptation

The same skill markdown is adapted by execution strategy:

| Strategy | How The Skill Is Supplied |
|---|---|
| Subprocess CLI | Session manager embeds the skill markdown and invocation into a self-contained prompt. |
| Harness/tool-chat | Harness receives the skill markdown as system context and the invocation as user content. |
| Legacy slash-command capable tools | Bundled skills are installed into the tool's command directory when supported. |

This lets provider drivers focus on transport and tool mechanics while skills stay focused on the work contract.

## Skill Authoring Rules

A bundled skill must:

- have YAML frontmatter with `name` and `description`
- describe inputs and outputs
- name required file paths and schemas
- define sentinels exactly
- define when to print `BLOCKED:`
- define verification gates
- avoid provider-specific commands unless the command is the product action itself, such as `gh`

A bundled skill should say “agent”, “worker”, or “provider” rather than naming a vendor unless it is documenting a compatibility path.

## Repo Instructions

Skills may tell agents to read repo instructions. The runner layer also appends known instruction files for harness providers.

Supported instruction locations:

- `AGENTS.md`
- `CLAUDE.md`
- `.codex/rules/*.md`
- `.claude/rules/*.md`

These paths are compatibility inputs, not a reason to fork skills.

## Tests

The bundle tests enforce:

- every bundled skill has Codex-compatible frontmatter
- provider-specific skill suffixes are disallowed

Run:

```bash
go test ./internal/skills/bundle/tests
```

When changing session invocation behavior, also run:

```bash
go test ./internal/session ./internal/harness
```

## Adding A New Skill

1. Add one canonical markdown file under `internal/skills/bundle/skills/`.
2. Use a vendor-neutral name, such as `conductor-review` instead of `conductor-review-codex`.
3. Include frontmatter:

   ```yaml
   ---
   name: conductor-review
   description: Reviews an implementation against the approved plan and emits PASS/FAIL findings for the pipeline.
   ---
   ```

4. Document inputs, outputs, sentinels, and blocking rules.
5. Add tests if the skill defines a schema or non-negotiable phrase.
6. Run bundle tests.

## Migrating Old Provider Forks

When replacing duplicate skill variants:

1. Pick the most complete variant as source material.
2. Merge useful instructions into the canonical skill.
3. Move provider mechanics into `internal/session`, `internal/harness`, or the relevant provider driver.
4. Delete the provider-specific skill file.
5. Remove installed local copies if needed.
6. Add a test that prevents the fork pattern from returning.
