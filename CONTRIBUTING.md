# Contributing

Thanks for working on PrismConductor.

Start here:

- [Contributing Guide](docs/contributing.md)
- [Architecture](docs/architecture.md)
- [Data Contracts](docs/data-contracts.md)
- [Agent-Neutral Skills](docs/agent-neutral-skills.md)
- [Roadmap](docs/roadmap.md)
- [Agent Rules](AGENTS.md)
- [Historical Product Plan](PRISMCONDUCTOR_PLAN.md)

Minimum local checks before a PR:

```bash
go test ./...
cd frontend && npx tsc --noEmit
```

For user-facing behavior changes, update the relevant docs under `docs/` in the same PR.
