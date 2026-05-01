-- Issue #101: per-session LLM token counts and estimated cost.
-- Applied by internal/store/sqlite.go migrate() using idempotent ALTER TABLE.
-- This file is documentation only; the Go migration is the authoritative source.

ALTER TABLE sessions ADD COLUMN input_tokens INTEGER NOT NULL DEFAULT 0;
ALTER TABLE sessions ADD COLUMN output_tokens INTEGER NOT NULL DEFAULT 0;
ALTER TABLE sessions ADD COLUMN estimated_cost_cents REAL NOT NULL DEFAULT 0;
