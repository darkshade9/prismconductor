package store

import (
	"database/sql"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"prismconductor/internal/store/migrations"

	_ "modernc.org/sqlite"
)

// openLegacyDB creates an in-memory (or file) SQLite DB from a SQL fixture file,
// simulating a database written by a pre-framework binary.
func openLegacyDB(t *testing.T, fixturePath string) *sql.DB {
	t.Helper()
	sqlBytes, err := os.ReadFile(fixturePath)
	if err != nil {
		t.Fatalf("read fixture %s: %v", fixturePath, err)
	}
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("open in-memory db: %v", err)
	}
	t.Cleanup(func() { db.Close() })

	// SQLite's multi-statement Exec support varies; split on semicolons.
	for _, stmt := range strings.Split(string(sqlBytes), ";") {
		stmt = strings.TrimSpace(stmt)
		if stmt == "" || strings.HasPrefix(stmt, "--") {
			continue
		}
		if _, err := db.Exec(stmt); err != nil {
			t.Fatalf("apply fixture stmt %q: %v", stmt, err)
		}
	}
	return db
}

// TestMigrationRunIdempotent verifies that calling migrations.Run twice does
// not error — every migration is guarded by the schema_migrations table so
// replaying is safe.
func TestMigrationRunIdempotent(t *testing.T) {
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	// Seed the settings table (needed for CheckVersion + schema_version stamp).
	if _, err := db.Exec(`CREATE TABLE IF NOT EXISTS settings (key TEXT PRIMARY KEY, value TEXT NOT NULL)`); err != nil {
		t.Fatal(err)
	}

	if err := migrations.Run(db); err != nil {
		t.Fatalf("first Run: %v", err)
	}
	if err := migrations.Run(db); err != nil {
		t.Fatalf("second Run (idempotent): %v", err)
	}
}

// TestMigrationBaselineRecorded verifies that after migrations.Run a fresh DB
// has the baseline migration ID in schema_migrations.
func TestMigrationBaselineRecorded(t *testing.T) {
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	if _, err := db.Exec(`CREATE TABLE IF NOT EXISTS settings (key TEXT PRIMARY KEY, value TEXT NOT NULL)`); err != nil {
		t.Fatal(err)
	}

	if err := migrations.Run(db); err != nil {
		t.Fatalf("Run: %v", err)
	}

	var count int
	if err := db.QueryRow(
		`SELECT COUNT(*) FROM schema_migrations WHERE id = ?`, migrations.MaxID(),
	).Scan(&count); err != nil {
		t.Fatalf("query schema_migrations: %v", err)
	}
	if count != 1 {
		t.Errorf("expected baseline migration recorded, got count=%d", count)
	}
}

// TestSchemaVersionStamped verifies that schema_version is written to settings.
func TestSchemaVersionStamped(t *testing.T) {
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	if _, err := db.Exec(`CREATE TABLE IF NOT EXISTS settings (key TEXT PRIMARY KEY, value TEXT NOT NULL)`); err != nil {
		t.Fatal(err)
	}

	if err := migrations.Run(db); err != nil {
		t.Fatal(err)
	}

	var version string
	if err := db.QueryRow(`SELECT value FROM settings WHERE key = 'schema_version'`).Scan(&version); err != nil {
		t.Fatalf("read schema_version: %v", err)
	}
	if version != migrations.MaxID() {
		t.Errorf("schema_version = %q, want %q", version, migrations.MaxID())
	}
}

// TestDowngradeDetected verifies that CheckVersion returns an error when the
// DB's schema_version exceeds the binary's known max migration ID.
func TestDowngradeDetected(t *testing.T) {
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	if _, err := db.Exec(`CREATE TABLE IF NOT EXISTS settings (key TEXT PRIMARY KEY, value TEXT NOT NULL)`); err != nil {
		t.Fatal(err)
	}
	// Stamp a future version that this binary doesn't know about.
	futureVersion := "99991231_99_future_migration"
	if _, err := db.Exec(
		`INSERT INTO settings (key, value) VALUES ('schema_version', ?)`,
		futureVersion,
	); err != nil {
		t.Fatal(err)
	}

	if err := migrations.CheckVersion(db); err == nil {
		t.Error("expected downgrade error, got nil")
	}
}

// TestCheckVersionAbsentOK verifies that CheckVersion is a no-op when
// schema_version is absent (pre-framework DB).
func TestCheckVersionAbsentOK(t *testing.T) {
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	if _, err := db.Exec(`CREATE TABLE IF NOT EXISTS settings (key TEXT PRIMARY KEY, value TEXT NOT NULL)`); err != nil {
		t.Fatal(err)
	}

	if err := migrations.CheckVersion(db); err != nil {
		t.Errorf("unexpected error for pre-framework DB: %v", err)
	}
}

// TestMigrateV0Baseline applies migrations.Run on top of a legacy v0 DB (the
// schema produced before the migration framework existed) and asserts no errors.
func TestMigrateV0Baseline(t *testing.T) {
	fixture := filepath.Join("testdata", "db_fixtures", "v0_baseline.sql")
	db := openLegacyDB(t, fixture)

	if err := migrations.Run(db); err != nil {
		t.Fatalf("Run on v0 baseline fixture: %v", err)
	}

	// schema_migrations must exist and contain the baseline entry.
	var count int
	if err := db.QueryRow(`SELECT COUNT(*) FROM schema_migrations`).Scan(&count); err != nil {
		t.Fatalf("query schema_migrations: %v", err)
	}
	if count == 0 {
		t.Error("no migrations recorded after Run on v0 baseline")
	}
}

// TestBackupCreated verifies that createBackup writes a .bak file next to the source.
func TestBackupCreated(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "conductor.db")
	if err := os.WriteFile(dbPath, []byte("fake db content"), 0o600); err != nil {
		t.Fatal(err)
	}

	if err := createBackup(dbPath, 3); err != nil {
		t.Fatalf("createBackup: %v", err)
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	var bakCount int
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), "conductor.db.pre-") && strings.HasSuffix(e.Name(), ".bak") {
			bakCount++
		}
	}
	if bakCount != 1 {
		t.Errorf("expected 1 backup file, got %d", bakCount)
	}
}

// TestBackupRotation verifies that old backups are pruned to the keep limit.
func TestBackupRotation(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "conductor.db")
	if err := os.WriteFile(dbPath, []byte("fake"), 0o600); err != nil {
		t.Fatal(err)
	}

	// Seed 4 existing backups with timestamps that sort lexicographically.
	timestamps := []string{"20240101T000000Z", "20240201T000000Z", "20240301T000000Z", "20240401T000000Z"}
	for _, ts := range timestamps {
		name := filepath.Join(dir, "conductor.db.pre-"+ts+".bak")
		if err := os.WriteFile(name, []byte("old"), 0o600); err != nil {
			t.Fatal(err)
		}
	}

	// createBackup with keep=3 should leave exactly 3 backups total.
	if err := createBackup(dbPath, 3); err != nil {
		t.Fatalf("createBackup: %v", err)
	}

	entries, _ := os.ReadDir(dir)
	var baks []string
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), "conductor.db.pre-") && strings.HasSuffix(e.Name(), ".bak") {
			baks = append(baks, e.Name())
		}
	}
	if len(baks) != 3 {
		t.Errorf("expected 3 backups after rotation, got %d: %v", len(baks), baks)
	}
}

// TestBackupNoopWhenMissing verifies that createBackup is a no-op when the
// source file does not exist (first run / fresh install).
func TestBackupNoopWhenMissing(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "conductor.db")
	// No file created — simulates first run.

	if err := createBackup(dbPath, 3); err != nil {
		t.Fatalf("createBackup on missing file: %v", err)
	}

	entries, _ := os.ReadDir(dir)
	if len(entries) != 0 {
		t.Errorf("expected no files in dir, got %d", len(entries))
	}
}
