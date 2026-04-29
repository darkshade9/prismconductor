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
	return nil
}
