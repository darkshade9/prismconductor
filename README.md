# PrismConductor

A goal-driven, multi-workspace agent orchestration desktop app.

See `PRISMCONDUCTOR_PLAN.md` for the full design.

## Stack

- **Wails v2** (Go backend + React/TS frontend, single binary)
- **Tailwind + shadcn/ui** for the board
- **dnd-kit** for drag-and-drop
- **modernc.org/sqlite** for local persistence
- **creack/pty** for spawning `claude` worker subprocesses
- **Ollama / Qwen 2.5 14B** as the orchestrator LLM (local, free)

## Develop

```bash
wails dev      # hot-reloading dev server
wails build    # produces build/bin/prismconductor.app
```

## Status

Day 1 of Phase 1 complete: scaffold + bound demo (`SpawnDemo` calls `claude --version` via PTY and streams output to the SessionDrawer). See open issues for remaining phases.
