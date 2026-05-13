# Cost expectations

This page explains how PrismConductor estimates and projects your monthly LLM spend.

## How projections work

PrismConductor records the token counts and estimated cost of every terminal session (completed, failed, or blocked). The **Spending overview** panel (Settings → Spending) uses these records to show you:

- **Last 7 days** — total spend in the rolling 7-day window ending now
- **Last 30 days** — total spend in the rolling 30-day window ending now
- **Projected month** — straight-line extrapolation: `(last-30d-total / days-with-data) × 30`

When a full 30 days of history exists, the projection equals the 30-day total. When you have fewer days of history (e.g., a new pool), the projection scales up proportionally.

## Typical workload costs

These ranges assume you run PrismConductor against a real development backlog with medium-complexity issues. Actual costs depend heavily on model choice, plan length, and code-base size.

| Worker type | Typical tokens | Estimated cost |
|---|---|---|
| Plan worker | 20,000–80,000 tokens | ~$0.10–$0.50 |
| Execute worker | 100,000–300,000 tokens | ~$0.50–$2.50 |
| Typical dev month (20–40 issues) | — | **$30–$100** |

Costs scale with model tier. Switching from a premium model (Claude Opus, GPT-4) to a mid-tier model (Claude Sonnet, GPT-4o-mini) can cut costs 5–10×.

## Free-tier and local pools

Pools backed by **Ollama**, **LM Studio**, or **Codex** (ChatGPT subscription CLI) run locally or via a subscription without per-token API billing. PrismConductor displays these as `$0/mo (free)` and excludes them from cost projections.

**Google Gemini** has a free tier (1,500 Flash requests/day, 50 Pro requests/day) and paid tiers. If you stay within the free quota, your actual spend is $0 and the projection will reflect that. Once you exceed the free quota, per-token charges apply and the projection updates accordingly.

## Color thresholds

| Color | Monthly projection |
|---|---|
| Green | < $50 |
| Amber | $50–$200 |
| Red | > $200 |

These thresholds can be adjusted in a future release.

## Caveats

- Projections are observational only — they do not enforce a budget or stop sessions.
- Token counts are measured from the LLM API response; costs are computed using the rates in Settings → Pools.
- If no rate is configured for a model, the cost is shown as `—`.
- Harness-mode sessions (non-Claude providers) use an estimated cost based on token counts and configured rates; actual billed amounts may differ.
