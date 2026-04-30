// Package store wraps SQLite persistence (PRISMCONDUCTOR_PLAN.md §13).
package store

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"strings"

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
	if err := s.migrate(); err != nil {
		db.Close()
		return nil, fmt.Errorf("migrate: %w", err)
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
