# Claude to Codex migration

This directory mirrors the repo-local Claude handoff data that existed at migration time.

## Copied

- `CLAUDE.md` -> `AGENTS.md`
- `.claude/settings.local.json` -> `.codex/settings.local.json`

## Not present in `.claude/`

The `.claude/` directory did not contain `skills/`, `commands/`, `rules/`, or `hooks/` at migration time, so there were no repo-local files in those categories to copy.

PrismConductor already discovers repo skills and commands from `.codex/skills/` and `.codex/commands/` via `internal/skills/discover.go`.
