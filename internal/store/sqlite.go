// Package store wraps SQLite persistence (PRISMCONDUCTOR_PLAN.md §13).
package store

import (
	"database/sql"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"

	"prismconductor/internal/store/migrations"

	_ "modernc.org/sqlite"
)

type Store struct {
	DB   *sql.DB
	Path string
}

// Open creates (if missing) and migrates the SQLite DB.
func Open(dir string) (*Store, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	path := filepath.Join(dir, "conductor.db")

	// Pre-migration backup: copy the existing DB file before any DDL runs.
	// Only runs when the file already exists (not a fresh install) and when
	// there are pending migrations to apply (avoids creating a backup on
	// every startup). A failed backup is logged but non-fatal — the user
	// can still open the DB; a backup failure must not block the app.
	if _, statErr := os.Stat(path); statErr == nil {
		// Peek at pending status without opening a full connection.
		if peekDB, err := sql.Open("sqlite", path); err == nil {
			if migrations.HasPending(peekDB) {
				peekDB.Close()
				if err := createBackup(path, 3); err != nil {
					log.Printf("store: pre-migration backup failed (non-fatal): %v", err)
				}
			} else {
				peekDB.Close()
			}
		}
	}

	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, err
	}
	// Concurrency settings. Default journal_mode=delete + busy_timeout=0
	// caused SQLITE_BUSY at startup when the poller, reconciler, orchestrator,
	// and workspace sync all hit the DB simultaneously: rollback-journal
	// locking is exclusive on writes and any contender returns immediately.
	//
	//   - WAL allows readers to run concurrently with a writer (and multiple
	//     writers are still serialized but block instead of erroring).
	//   - busy_timeout retries a busy lock for up to 5s before giving up,
	//     which absorbs the startup-burst contention spike.
	//   - synchronous=NORMAL is the recommended pairing with WAL — durable
	//     across power loss minus the last few transactions, fsync-on-checkpoint
	//     instead of fsync-per-commit.
	for _, pragma := range []string{
		"PRAGMA journal_mode = WAL",
		"PRAGMA busy_timeout = 5000",
		"PRAGMA synchronous = NORMAL",
		"PRAGMA foreign_keys = ON",
	} {
		if _, err := db.Exec(pragma); err != nil {
			db.Close()
			return nil, fmt.Errorf("apply %s: %w", pragma, err)
		}
	}
	s := &Store{DB: db, Path: path}

	// Legacy idempotent DDL (pre-framework migrations).
	if err := s.migrate(); err != nil {
		db.Close()
		return nil, fmt.Errorf("migrate: %w", err)
	}

	// Downgrade guard: refuse to open a DB written by a newer binary.
	if err := migrations.CheckVersion(db); err != nil {
		db.Close()
		return nil, err
	}

	// Versioned migration framework: creates schema_migrations, applies pending
	// migrations, stamps schema_version.
	if err := migrations.Run(db); err != nil {
		db.Close()
		return nil, fmt.Errorf("schema migration: %w", err)
	}

	return s, nil
}

func (s *Store) Close() error { return s.DB.Close() }

func (s *Store) migrate() error {
	_, err := s.DB.Exec(`
CREATE TABLE IF NOT EXISTS workspaces (
    id TEXT PRIMARY KEY,
    json TEXT NOT NULL
);
CREATE TABLE IF NOT EXISTS goals (
    id TEXT PRIMARY KEY,
    workspace_id TEXT,
    status TEXT NOT NULL,
    json TEXT NOT NULL,
    "order" INTEGER NOT NULL DEFAULT 0
);
CREATE TABLE IF NOT EXISTS issues (
    workspace_id TEXT NOT NULL,
    number INTEGER NOT NULL,
    column_name TEXT NOT NULL,
    manual_order INTEGER,
    json TEXT NOT NULL,
    PRIMARY KEY (workspace_id, number)
);
CREATE TABLE IF NOT EXISTS plans (
    workspace_id TEXT NOT NULL,
    issue_number INTEGER NOT NULL,
    revision INTEGER NOT NULL,
    json TEXT NOT NULL,
    PRIMARY KEY (workspace_id, issue_number, revision)
);
CREATE TABLE IF NOT EXISTS sessions (
    id TEXT PRIMARY KEY,
    workspace_id TEXT NOT NULL,
    issue_number INTEGER NOT NULL,
    pid INTEGER,
    state TEXT NOT NULL,
    transcript_path TEXT,
    pending_question_id TEXT,
    json TEXT NOT NULL
);
CREATE TABLE IF NOT EXISTS events (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    type TEXT NOT NULL,
    ts INTEGER NOT NULL,
    payload TEXT
);
CREATE TABLE IF NOT EXISTS settings (
    key TEXT PRIMARY KEY,
    value TEXT NOT NULL
);
CREATE TABLE IF NOT EXISTS labels (
    workspace_id TEXT NOT NULL,
    name         TEXT NOT NULL,
    color        TEXT NOT NULL,
    description  TEXT,
    PRIMARY KEY (workspace_id, name)
);
CREATE TABLE IF NOT EXISTS pools (
    id           TEXT PRIMARY KEY,
    name         TEXT NOT NULL,
    provider     TEXT NOT NULL,
    endpoint     TEXT NOT NULL DEFAULT '',
    model        TEXT NOT NULL,
    capacity     INTEGER NOT NULL DEFAULT 1,
    enabled      INTEGER NOT NULL DEFAULT 1,
    api_key      TEXT NOT NULL DEFAULT '',
    role         TEXT NOT NULL DEFAULT 'work' CHECK(role IN ('plan','work','orchestrator')),
    created_at   INTEGER NOT NULL
);
`)
	if err != nil {
		return err
	}

	// Idempotent additive column for issue archiving (#34). Unix seconds; NULL = not archived.
	if _, err := s.DB.Exec(`ALTER TABLE issues ADD COLUMN archived_at INTEGER`); err != nil {
		if !strings.Contains(err.Error(), "duplicate column name") {
			return err
		}
	}
	// Idempotent additive column for mid-run question pause (#17). NULL when no
	// question is in flight; UUID of the pending question otherwise.
	if _, err := s.DB.Exec(`ALTER TABLE sessions ADD COLUMN pending_question_id TEXT`); err != nil {
		if !strings.Contains(err.Error(), "duplicate column name") {
			return err
		}
	}
	// Issue #39: role tag on each pool. Idempotent additive column with default
	// 'work' so pre-#39 rows continue to behave as work pools.
	if _, err := s.DB.Exec(`ALTER TABLE pools ADD COLUMN role TEXT NOT NULL DEFAULT 'work'`); err != nil {
		if !strings.Contains(err.Error(), "duplicate column name") {
			return err
		}
	}
	if err := s.ensurePoolsRoleCheck(); err != nil {
		return err
	}
	// Issue #54: per-session byte offset of the last-flushed transcript line.
	// Lets a re-attach after conductor restart skip lines we already processed
	// in a prior catch-up pass.
	if _, err := s.DB.Exec(`ALTER TABLE sessions ADD COLUMN transcript_offset INTEGER NOT NULL DEFAULT 0`); err != nil {
		if !strings.Contains(err.Error(), "duplicate column name") {
			return err
		}
	}
	// Issue #40: pool preference ordering. Default 0 keeps existing pools
	// at equal priority (unchanged behaviour until the user reorders).
	if _, err := s.DB.Exec(`ALTER TABLE pools ADD COLUMN priority INTEGER NOT NULL DEFAULT 0`); err != nil {
		if !strings.Contains(err.Error(), "duplicate column name") {
			return err
		}
	}
	// Issue #40: persisted pending-pool queue so restart doesn't lose intent.
	if _, err := s.DB.Exec(`CREATE TABLE IF NOT EXISTS pending_pool_for (
    id           INTEGER PRIMARY KEY AUTOINCREMENT,
    workspace_id TEXT NOT NULL,
    issue_number INTEGER NOT NULL,
    role         TEXT NOT NULL,
    action       TEXT NOT NULL,
    created_at   INTEGER NOT NULL,
    UNIQUE(workspace_id, issue_number, role, action)
)`); err != nil {
		return err
	}
	// Issue #52: last-seen rate-limit snapshot per (pool, window). Overwrite-only
	// store — no historical rows.
	if _, err := s.DB.Exec(`CREATE TABLE IF NOT EXISTS pool_usage (
    pool_id     TEXT NOT NULL,
    window      TEXT NOT NULL,
    limit_value INTEGER NOT NULL,
    used        INTEGER NOT NULL,
    resets_at   INTEGER NOT NULL,
    captured_at INTEGER NOT NULL,
    PRIMARY KEY (pool_id, window)
)`); err != nil {
		return err
	}
	// Issue #85: optional per-pool temperature override. NULL = omit from request
	// (provider uses its default). Non-null = send verbatim (including 0.0).
	if _, err := s.DB.Exec(`ALTER TABLE pools ADD COLUMN temperature REAL`); err != nil {
		if !strings.Contains(err.Error(), "duplicate column name") {
			return err
		}
	}
	// Issue #89: per-pool harness budget overrides. NULL = use DefaultBudget() value.
	for _, col := range []string{
		`ALTER TABLE pools ADD COLUMN max_turns INTEGER`,
		`ALTER TABLE pools ADD COLUMN max_input_tokens INTEGER`,
		`ALTER TABLE pools ADD COLUMN bash_timeout_ms INTEGER`,
		`ALTER TABLE pools ADD COLUMN output_cap INTEGER`,
	} {
		if _, err := s.DB.Exec(col); err != nil {
			if !strings.Contains(err.Error(), "duplicate column name") {
				return err
			}
		}
	}
	// Issue #88: acknowledged_at records when the user cleared a blocked/failed
	// session. NULL = not yet acknowledged; non-null = Unix epoch of the clear.
	// The session row is preserved for audit; the UI ignores acknowledged rows.
	if _, err := s.DB.Exec(`ALTER TABLE sessions ADD COLUMN acknowledged_at INTEGER`); err != nil {
		if !strings.Contains(err.Error(), "duplicate column name") {
			return err
		}
	}
	// Issue #107: closed_at records when the GitHub issue was closed (Unix
	// seconds; NULL = open or pre-feature). Used by ArchiveClosedByAge to prune
	// DONE cards after N days.
	if _, err := s.DB.Exec(`ALTER TABLE issues ADD COLUMN closed_at INTEGER`); err != nil {
		if !strings.Contains(err.Error(), "duplicate column name") {
			return err
		}
	}
	// Issue #101: per-session LLM token counts and estimated cost persisted at
	// session end. Dedicated columns enable fast per-pool/per-goal aggregation
	// without unmarshalling every JSON blob. Default 0 so existing rows read 0.
	for _, col := range []string{
		`ALTER TABLE sessions ADD COLUMN input_tokens INTEGER NOT NULL DEFAULT 0`,
		`ALTER TABLE sessions ADD COLUMN output_tokens INTEGER NOT NULL DEFAULT 0`,
		`ALTER TABLE sessions ADD COLUMN estimated_cost_cents REAL NOT NULL DEFAULT 0`,
	} {
		if _, err := s.DB.Exec(col); err != nil {
			if !strings.Contains(err.Error(), "duplicate column name") {
				return err
			}
		}
	}
	// One-time backfill: sync `$.state` inside the JSON blob to the indexed
	// `state` column for any drifted rows. Pre-fix (UpdateSessionState only
	// touched the indexed column), every terminal transition left
	// json.state="running" while the indexed column moved to
	// completed/failed/blocked. The IssueView assembler reads state from
	// the JSON blob, so those rows surfaced as zombie active sessions
	// forever. Idempotent: WHERE filter no-ops once everything is in sync.
	if _, err := s.DB.Exec(
		`UPDATE sessions
		    SET json = json_set(json, '$.state', state)
		  WHERE json_extract(json, '$.state') IS NOT NULL
		    AND json_extract(json, '$.state') <> state`); err != nil {
		return fmt.Errorf("backfill session json.state: %w", err)
	}
	// Issue #109: pool scoping — shared vs workspace-specific.
	// Default 'shared' keeps existing rows visible to all workspaces unchanged.
	for _, col := range []string{
		`ALTER TABLE pools ADD COLUMN scope TEXT NOT NULL DEFAULT 'shared'`,
		`ALTER TABLE pools ADD COLUMN workspace_id TEXT NOT NULL DEFAULT ''`,
	} {
		if _, err := s.DB.Exec(col); err != nil {
			if !strings.Contains(err.Error(), "duplicate column name") {
				return err
			}
		}
	}
	// Issue #159: PR comment surface — persisted per (workspace, issue, comment_id).
	// conversation = issue thread comments; review = PR inline diff comments.
	// Issue #268: nullable provider_id FK on pools; providers table created by
	// the versioned migration framework (20260511_00_add_providers).
	if _, err := s.DB.Exec(`ALTER TABLE pools ADD COLUMN provider_id TEXT`); err != nil {
		if !strings.Contains(err.Error(), "duplicate column name") {
			return err
		}
	}
	if _, err := s.DB.Exec(`CREATE TABLE IF NOT EXISTS pr_comments (
    id           INTEGER PRIMARY KEY AUTOINCREMENT,
    workspace_id TEXT NOT NULL,
    issue_number INTEGER NOT NULL,
    comment_id   INTEGER NOT NULL,
    author       TEXT NOT NULL,
    body         TEXT NOT NULL,
    kind         TEXT NOT NULL,
    file_path    TEXT,
    line_number  INTEGER,
    created_at   INTEGER NOT NULL,
    read_at      INTEGER,
    pending_post INTEGER NOT NULL DEFAULT 0,
    UNIQUE(workspace_id, issue_number, comment_id)
);
CREATE INDEX IF NOT EXISTS idx_pr_comments_ws_issue ON pr_comments(workspace_id, issue_number);
`); err != nil {
		return err
	}
	return nil
}

// ensurePoolsRoleCheck rebuilds the pools table once with a CHECK constraint
// on role. SQLite's ALTER TABLE can't add a CHECK to an existing column, so
// the standard CREATE NEW + INSERT … SELECT + DROP + RENAME swap is gated on a
// one-time settings flag.
func (s *Store) ensurePoolsRoleCheck() error {
	done, _ := s.GetSetting("pools_role_check_v1")
	if done == "1" {
		return nil
	}
	var ddl string
	if err := s.DB.QueryRow(`SELECT sql FROM sqlite_master WHERE type='table' AND name='pools'`).Scan(&ddl); err != nil {
		return err
	}
	if strings.Contains(ddl, "CHECK(role IN") {
		return s.SetSetting("pools_role_check_v1", "1")
	}
	tx, err := s.DB.Begin()
	if err != nil {
		return err
	}
	rollback := func() { _ = tx.Rollback() }
	_, err = tx.Exec(`CREATE TABLE pools_new (
    id           TEXT PRIMARY KEY,
    name         TEXT NOT NULL,
    provider     TEXT NOT NULL,
    endpoint     TEXT NOT NULL DEFAULT '',
    model        TEXT NOT NULL,
    capacity     INTEGER NOT NULL DEFAULT 1,
    enabled      INTEGER NOT NULL DEFAULT 1,
    api_key      TEXT NOT NULL DEFAULT '',
    role         TEXT NOT NULL CHECK(role IN ('plan','work','orchestrator')),
    created_at   INTEGER NOT NULL
)`)
	if err != nil {
		rollback()
		return err
	}
	if _, err := tx.Exec(`INSERT INTO pools_new (id,name,provider,endpoint,model,capacity,enabled,api_key,role,created_at)
        SELECT id,name,provider,endpoint,model,capacity,enabled,api_key,COALESCE(role,'work'),created_at FROM pools`); err != nil {
		rollback()
		return err
	}
	if _, err := tx.Exec(`DROP TABLE pools`); err != nil {
		rollback()
		return err
	}
	if _, err := tx.Exec(`ALTER TABLE pools_new RENAME TO pools`); err != nil {
		rollback()
		return err
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	return s.SetSetting("pools_role_check_v1", "1")
}
