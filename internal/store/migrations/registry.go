package migrations

import (
	"database/sql"
	"strings"
)

// isAlreadyExists returns true when the SQLite error text indicates the column
// or table already exists (ALTER TABLE on an existing column).
func isAlreadyExists(err error) bool {
	return err != nil && strings.Contains(err.Error(), "duplicate column name")
}

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
	{
		ID:          "20260507_00_add_cross_workspace_deps",
		Description: "issue #208 Phase B: cross-workspace issue dependency index (CrossDeps stored in issue JSON; this table enables efficient reverse-lookup and future multi-instance coordination)",
		SQL: `
CREATE TABLE IF NOT EXISTS cross_workspace_deps (
    workspace_id      TEXT    NOT NULL,
    issue_number      INTEGER NOT NULL,
    dep_workspace_id  TEXT    NOT NULL,
    dep_issue_number  INTEGER NOT NULL,
    PRIMARY KEY (workspace_id, issue_number, dep_workspace_id, dep_issue_number)
);
CREATE INDEX IF NOT EXISTS idx_cwd_dep
    ON cross_workspace_deps (dep_workspace_id, dep_issue_number);`,
	},
	{
		ID:          "20260511_00_add_providers",
		Description: "issue #268: centralized provider credential entities; nullable provider_id FK on pools",
		SQL: `
CREATE TABLE IF NOT EXISTS providers (
    id         TEXT    PRIMARY KEY,
    name       TEXT    NOT NULL,
    kind       TEXT    NOT NULL,
    endpoint   TEXT    NOT NULL DEFAULT '',
    api_key    TEXT    NOT NULL DEFAULT '',
    created_at INTEGER NOT NULL
);`,
		Up: func(tx *sql.Tx) error {
			// The pools table is created by legacy migrate(), which runs before
			// the versioned migration framework on existing DBs. On a bare DB
			// (e.g. migration-framework tests) the table may not exist yet; in
			// that case the ALTER TABLE is a no-op and the legacy path will add
			// the column via its own idempotent ALTER TABLE on first open.
			var tblCount int
			if err := tx.QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name='pools'`).Scan(&tblCount); err != nil {
				return err
			}
			if tblCount == 0 {
				return nil
			}
			_, err := tx.Exec(`ALTER TABLE pools ADD COLUMN provider_id TEXT`)
			if err != nil && !isAlreadyExists(err) {
				return err
			}
			return nil
		},
	},
	{
		ID:          "20260513_00_add_pool_daily_usage",
		Description: "issue #283: per-pool daily usage aggregates for cost projection",
		SQL: `
CREATE TABLE IF NOT EXISTS pool_daily_usage (
    pool_id  TEXT NOT NULL,
    date     TEXT NOT NULL,
    sessions INTEGER NOT NULL DEFAULT 0,
    tokens   INTEGER NOT NULL DEFAULT 0,
    cents    REAL    NOT NULL DEFAULT 0,
    PRIMARY KEY (pool_id, date)
);
CREATE INDEX IF NOT EXISTS idx_pdu_pool_date ON pool_daily_usage (pool_id, date);`,
	},
	{
		ID:          "20260513_01_add_skill_outcomes",
		Description: "issue #293: per-session skill outcome log for self-improving skills (phase A)",
		SQL: `
CREATE TABLE IF NOT EXISTS skill_outcomes (
    session_id      TEXT    PRIMARY KEY,
    workspace_id    TEXT    NOT NULL,
    issue_number    INTEGER NOT NULL,
    skill_path      TEXT    NOT NULL,
    skill_hash      TEXT    NOT NULL,
    mode            TEXT    NOT NULL,
    outcome         TEXT    NOT NULL,
    blocked_reason  TEXT    NOT NULL DEFAULT '',
    user_action     TEXT    NOT NULL DEFAULT '',
    cost_cents      REAL    NOT NULL DEFAULT 0,
    duration_ms     INTEGER NOT NULL DEFAULT 0,
    transcript_path TEXT    NOT NULL DEFAULT '',
    captured_at     INTEGER NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_skill_outcomes_skill_time
    ON skill_outcomes(skill_path, captured_at);
CREATE INDEX IF NOT EXISTS idx_skill_outcomes_workspace
    ON skill_outcomes(workspace_id, captured_at);`,
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
