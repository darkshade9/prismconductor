-- issue #209: workspace collections + shared context (Phase A of #208)
-- Runtime migration lives in internal/store/migrations/registry.go (ID: 20260506_00_add_collections).
-- This file is the loose-file reference copy kept for schema archaeology.

CREATE TABLE IF NOT EXISTS collections (
    id          TEXT    PRIMARY KEY,
    name        TEXT    NOT NULL,
    context_md  TEXT    NOT NULL DEFAULT '',
    created_at  INTEGER NOT NULL,
    updated_at  INTEGER NOT NULL
);

CREATE TABLE IF NOT EXISTS collection_members (
    collection_id TEXT    NOT NULL,
    workspace_id  TEXT    NOT NULL,
    position      INTEGER NOT NULL DEFAULT 0,
    PRIMARY KEY (collection_id, workspace_id),
    FOREIGN KEY (collection_id) REFERENCES collections(id) ON DELETE CASCADE
);

-- Enforces v1 single-collection-per-workspace rule at the DDL layer.
CREATE UNIQUE INDEX IF NOT EXISTS idx_collection_members_workspace
    ON collection_members(workspace_id);
