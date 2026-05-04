-- Issue #109: pool scoping — shared vs workspace-specific.
-- Applied by internal/store/sqlite.go migrate() using idempotent ALTER TABLE.
-- This file is documentation only; the Go migration is the authoritative source.

ALTER TABLE pools ADD COLUMN scope TEXT NOT NULL DEFAULT 'shared';
ALTER TABLE pools ADD COLUMN workspace_id TEXT NOT NULL DEFAULT '';
