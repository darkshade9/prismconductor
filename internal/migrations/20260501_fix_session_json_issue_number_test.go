package migrations_test

import (
	"database/sql"
	"testing"

	"prismconductor/internal/migrations"
	_ "modernc.org/sqlite"
)

func openTestDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("sql.Open: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	if _, err := db.Exec(`CREATE TABLE sessions (
		id           TEXT PRIMARY KEY,
		workspace_id TEXT NOT NULL,
		issue_number INTEGER NOT NULL,
		state        TEXT NOT NULL DEFAULT 'running',
		json         TEXT NOT NULL
	)`); err != nil {
		t.Fatalf("create sessions table: %v", err)
	}
	return db
}

func insertSession(t *testing.T, db *sql.DB, id string, col, jsonNum int) {
	t.Helper()
	raw := `{"id":"` + id + `","issue_number":` + itoa(jsonNum) + `}`
	if _, err := db.Exec(`INSERT INTO sessions (id,workspace_id,issue_number,json) VALUES (?,?,?,?)`,
		id, "ws1", col, raw); err != nil {
		t.Fatalf("insertSession: %v", err)
	}
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	neg := ""
	if n < 0 {
		neg = "-"
		n = -n
	}
	digits := []byte{}
	for n > 0 {
		digits = append([]byte{byte('0' + n%10)}, digits...)
		n /= 10
	}
	return neg + string(digits)
}

func TestAuditSessionIssueNumbers_ConsistentDB(t *testing.T) {
	db := openTestDB(t)
	insertSession(t, db, "s1", 1, 1) // column == JSON: consistent
	insertSession(t, db, "s2", 2, 2) // consistent

	mismatches, err := migrations.AuditSessionIssueNumbers(db)
	if err != nil {
		t.Fatalf("AuditSessionIssueNumbers: %v", err)
	}
	if len(mismatches) != 0 {
		t.Errorf("expected 0 mismatches on consistent DB, got %d: %v", len(mismatches), mismatches)
	}
}

func TestAuditSessionIssueNumbers_DetectsMismatch(t *testing.T) {
	db := openTestDB(t)
	insertSession(t, db, "ok", 1, 1)   // consistent
	insertSession(t, db, "bad", 2, 99) // column=2 but JSON says 99 — the bug

	mismatches, err := migrations.AuditSessionIssueNumbers(db)
	if err != nil {
		t.Fatalf("AuditSessionIssueNumbers: %v", err)
	}
	if len(mismatches) != 1 {
		t.Fatalf("expected 1 mismatch, got %d: %v", len(mismatches), mismatches)
	}
	if mismatches[0].ID != "bad" {
		t.Errorf("mismatch ID = %q, want bad", mismatches[0].ID)
	}
	if mismatches[0].ColumnNumber != 2 {
		t.Errorf("ColumnNumber = %d, want 2", mismatches[0].ColumnNumber)
	}
	if mismatches[0].JSONNumber != 99 {
		t.Errorf("JSONNumber = %d, want 99", mismatches[0].JSONNumber)
	}
}

func TestFixSessionIssueNumbers_RepairsJSON(t *testing.T) {
	db := openTestDB(t)
	insertSession(t, db, "bad", 3, 7) // column=3 authoritative, JSON wrong

	n, err := migrations.FixSessionIssueNumbers(db)
	if err != nil {
		t.Fatalf("FixSessionIssueNumbers: %v", err)
	}
	if n != 1 {
		t.Errorf("fixed count = %d, want 1", n)
	}

	// After fix, audit must report no mismatches.
	mismatches, err := migrations.AuditSessionIssueNumbers(db)
	if err != nil {
		t.Fatalf("re-audit: %v", err)
	}
	if len(mismatches) != 0 {
		t.Errorf("expected 0 mismatches after fix, got %d: %v", len(mismatches), mismatches)
	}
}

// TestCIAudit_FailsOnMismatch is the CI guard: if any sessions in the DB have
// a mismatched issue_number, this test fails loudly.
func TestCIAudit_FailsOnMismatch(t *testing.T) {
	db := openTestDB(t)
	// A clean DB with consistent sessions must pass the audit.
	insertSession(t, db, "s1", 10, 10)
	insertSession(t, db, "s2", 20, 20)

	mismatches, err := migrations.AuditSessionIssueNumbers(db)
	if err != nil {
		t.Fatalf("audit: %v", err)
	}
	if len(mismatches) != 0 {
		t.Errorf("CI AUDIT FAILED: %d session(s) have mismatched issue_number:\n%v\nRun migrations.FixSessionIssueNumbers to repair.", len(mismatches), mismatches)
	}
}
