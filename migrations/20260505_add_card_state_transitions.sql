-- Issue #30: card_state_transitions audit table.
-- Every call to cardstate.ApplyEvent writes one row so state changes are
-- forensically queryable per-issue.
-- Applied by internal/store/migrations/registry.go.

CREATE TABLE IF NOT EXISTS card_state_transitions (
    id              INTEGER PRIMARY KEY AUTOINCREMENT,
    workspace_id    TEXT    NOT NULL,
    issue_number    INTEGER NOT NULL,
    from_state      TEXT    NOT NULL,
    to_state        TEXT    NOT NULL,
    event_kind      TEXT    NOT NULL,
    details_json    TEXT,
    transitioned_at INTEGER NOT NULL -- Unix epoch seconds
);

CREATE INDEX IF NOT EXISTS idx_cst_workspace_issue
    ON card_state_transitions (workspace_id, issue_number);
