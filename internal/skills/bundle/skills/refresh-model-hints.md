---
name: refresh-model-hints
description: Fetches the hermesguide.xyz model capability index, normalizes it, and writes internal/llm/external_models.json. Run this periodically to keep the external snapshot current. Commit the result to ship updated hints to users.
---

# refresh-model-hints

Refreshes the external model capability snapshot at `internal/llm/external_models.json` by
fetching `hermesguide.xyz`. The bundled matrix in `internal/llm/models.go` always takes
precedence over this snapshot at runtime; the snapshot only adds coverage for models not
in the bundled list.

## Usage

```bash
# Via Make
make refresh-model-hints

# Directly
go run internal/llm/refresh_external.go

# Custom output path
go run internal/llm/refresh_external.go --output /path/to/external_models.json
```

## What it does

1. Attempts `GET https://hermesguide.xyz/api/models` (JSON endpoint).
2. Falls back to HTML parsing of `https://hermesguide.xyz` if the API is unavailable.
3. Normalizes fields into the `ModelHint` shape.
4. Writes `internal/llm/external_models.json` with a `fetched_at` timestamp.
5. Exits non-zero on parse failure **without removing** the existing snapshot.

## After refreshing

Commit `internal/llm/external_models.json` to ship the updated hints:

```bash
git add internal/llm/external_models.json
git commit -m "chore(llm): refresh external model hints snapshot"
```

## Precedence rules

| Source   | Priority | When used                                     |
|----------|----------|-----------------------------------------------|
| bundled  | 1 (high) | Always — hand-curated, reviewed in-repo       |
| external | 2 (low)  | Fallback for models not in the bundled matrix |

When a model appears in both sources, the bundled entry wins and the UI tooltip notes
"bundled entry overrides external."
