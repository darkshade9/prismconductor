// Package migrations provides one-time DB repair and audit helpers. Each file
// is named <date>_<description>.go and exports a small, self-contained set of
// functions that tests can call directly to act as CI guards.
package migrations

import (
	"database/sql"
	"encoding/json"
	"fmt"
)

// SessionMismatch describes a session row where the issue_number column
// disagrees with the issue_number field embedded in the JSON blob. The column
// is the canonical source of truth (it is set by explicit Go code and never
// derived from the blob). A mismatch means the blob was saved with a wrong
// IssueNumber value, which causes the assembler to fetch that session for the
// wrong issue on every subsequent restart.
type SessionMismatch struct {
	ID           string
	WorkspaceID  string
	ColumnNumber int
	JSONNumber   int
}

// AuditSessionIssueNumbers returns every session row whose issue_number column
// does not match json_extract(json, '$.issue_number'). An empty slice means
// the DB is consistent.
func AuditSessionIssueNumbers(db *sql.DB) ([]SessionMismatch, error) {
	rows, err := db.Query(`
		SELECT id, workspace_id, issue_number,
		       CAST(json_extract(json, '$.issue_number') AS INTEGER) AS json_num
		FROM sessions
		WHERE issue_number != CAST(json_extract(json, '$.issue_number') AS INTEGER)
	`)
	if err != nil {
		return nil, fmt.Errorf("audit query: %w", err)
	}
	defer rows.Close()

	var out []SessionMismatch
	for rows.Next() {
		var m SessionMismatch
		if err := rows.Scan(&m.ID, &m.WorkspaceID, &m.ColumnNumber, &m.JSONNumber); err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

// FixSessionIssueNumbers repairs mismatched rows by rewriting the JSON blob
// so its issue_number matches the column. Returns the number of rows patched.
func FixSessionIssueNumbers(db *sql.DB) (int, error) {
	mismatches, err := AuditSessionIssueNumbers(db)
	if err != nil {
		return 0, err
	}
	fixed := 0
	for _, m := range mismatches {
		var raw string
		if err := db.QueryRow(`SELECT json FROM sessions WHERE id = ?`, m.ID).Scan(&raw); err != nil {
			return fixed, fmt.Errorf("read session %s: %w", m.ID, err)
		}
		var blob map[string]any
		if err := json.Unmarshal([]byte(raw), &blob); err != nil {
			return fixed, fmt.Errorf("unmarshal session %s: %w", m.ID, err)
		}
		blob["issue_number"] = m.ColumnNumber
		patched, err := json.Marshal(blob)
		if err != nil {
			return fixed, fmt.Errorf("marshal session %s: %w", m.ID, err)
		}
		if _, err := db.Exec(`UPDATE sessions SET json = ? WHERE id = ?`, string(patched), m.ID); err != nil {
			return fixed, fmt.Errorf("update session %s: %w", m.ID, err)
		}
		fixed++
	}
	return fixed, nil
}
