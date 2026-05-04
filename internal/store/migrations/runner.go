package migrations

import (
	"database/sql"
	"fmt"
	"time"
)

const createSchemamigrations = `
CREATE TABLE IF NOT EXISTS schema_migrations (
    id         TEXT    PRIMARY KEY,
    applied_at INTEGER NOT NULL
);`

// HasPending reports whether any registered migration has not yet been applied.
// Returns true whenever schema_migrations does not exist (fresh or pre-framework DB).
func HasPending(db *sql.DB) bool {
	var count int
	if err := db.QueryRow("SELECT COUNT(*) FROM schema_migrations").Scan(&count); err != nil {
		return true // table doesn't exist → migrations pending
	}
	for _, m := range All() {
		var exists int
		_ = db.QueryRow("SELECT COUNT(*) FROM schema_migrations WHERE id = ?", m.ID).Scan(&exists)
		if exists == 0 {
			return true
		}
	}
	return false
}

// CheckVersion reads the schema_version from the settings table and returns an
// error if the DB was written by a newer binary than the current one.
// Returns nil if schema_version is absent (pre-framework DB).
func CheckVersion(db *sql.DB) error {
	var version string
	err := db.QueryRow("SELECT value FROM settings WHERE key = 'schema_version'").Scan(&version)
	if err != nil {
		return nil // missing = pre-framework DB, always acceptable
	}
	max := MaxID()
	if max == "" {
		return nil
	}
	if version > max {
		return fmt.Errorf(
			"conductor.db schema version %q is newer than this binary (max known: %q);\n"+
				"upgrade PrismConductor to at least the version that introduced %q,\n"+
				"or restore from a backup created before the upgrade (see RECOVERY.md)",
			version, max, version,
		)
	}
	return nil
}

// Run creates the schema_migrations table if absent, applies any pending
// migrations in lexicographic order, and stamps schema_version in settings.
func Run(db *sql.DB) error {
	if _, err := db.Exec(createSchemamigrations); err != nil {
		return fmt.Errorf("create schema_migrations: %w", err)
	}

	for _, m := range All() {
		var exists int
		_ = db.QueryRow("SELECT COUNT(*) FROM schema_migrations WHERE id = ?", m.ID).Scan(&exists)
		if exists > 0 {
			continue
		}

		tx, err := db.Begin()
		if err != nil {
			return fmt.Errorf("migration %s: begin: %w", m.ID, err)
		}

		if m.SQL != "" {
			if _, err := tx.Exec(m.SQL); err != nil {
				_ = tx.Rollback()
				return fmt.Errorf("migration %s: sql: %w", m.ID, err)
			}
		}
		if m.Up != nil {
			if err := m.Up(tx); err != nil {
				_ = tx.Rollback()
				return fmt.Errorf("migration %s: up: %w", m.ID, err)
			}
		}

		if _, err := tx.Exec(
			"INSERT INTO schema_migrations (id, applied_at) VALUES (?, ?)",
			m.ID, time.Now().Unix(),
		); err != nil {
			_ = tx.Rollback()
			return fmt.Errorf("migration %s: record: %w", m.ID, err)
		}
		if err := tx.Commit(); err != nil {
			return fmt.Errorf("migration %s: commit: %w", m.ID, err)
		}
	}

	// Stamp the DB with the highest applied migration ID.
	maxID := MaxID()
	if maxID != "" {
		_, err := db.Exec(
			"INSERT INTO settings (key, value) VALUES ('schema_version', ?) "+
				"ON CONFLICT(key) DO UPDATE SET value = excluded.value",
			maxID,
		)
		if err != nil {
			return fmt.Errorf("stamp schema_version: %w", err)
		}
	}
	return nil
}
