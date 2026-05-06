package migrations

// all is the ordered list of every migration this binary knows about.
// IDs must be lexicographically ordered (YYYYMMDD_NN_description).
// Never remove or reorder entries; only append.
var all = []Migration{
	{
		ID:          "20250504_00_initial_migration_framework",
		Description: "baseline: record all pre-framework schema (created by legacy migrate()) as applied",
		// No SQL/Up — the schema already exists via the legacy migrate() call.
		// Recording this entry means any future binary can detect a downgrade.
	},
	{
		ID:          "20260505_00_add_card_state_transitions",
		Description: "issue #30: add card_state_transitions audit table for forensic state-change queries",
		SQL: `
CREATE TABLE IF NOT EXISTS card_state_transitions (
    id              INTEGER PRIMARY KEY AUTOINCREMENT,
    workspace_id    TEXT    NOT NULL,
    issue_number    INTEGER NOT NULL,
    from_state      TEXT    NOT NULL,
    to_state        TEXT    NOT NULL,
    event_kind      TEXT    NOT NULL,
    details_json    TEXT,
    transitioned_at INTEGER NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_cst_workspace_issue
    ON card_state_transitions (workspace_id, issue_number);`,
	},
	{
		ID:          "20260506_00_add_collections",
		Description: "issue #209: workspace collections + shared context (Phase A of #208)",
		SQL: `
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
CREATE UNIQUE INDEX IF NOT EXISTS idx_collection_members_workspace
    ON collection_members(workspace_id);`,
	},
}

// All returns every known migration in application order.
func All() []Migration { return all }

// MaxID returns the highest known migration ID (the binary's schema level).
func MaxID() string {
	if len(all) == 0 {
		return ""
	}
	return all[len(all)-1].ID
}
