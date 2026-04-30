package store

import (
	"database/sql"
	"encoding/json"
	"errors"
	"time"

	"prismconductor/internal/types"
)

// LoadIssue returns the issue row for (workspaceID, number). Used by the
// execute spawn path (issue #22) so the branch slug can be derived from the
// issue title rather than a stub Issue{Number, WorkspaceID}.
func (s *Store) LoadIssue(workspaceID string, number int) (types.Issue, error) {
	if s == nil || s.DB == nil {
		return types.Issue{}, errors.New("store unavailable")
	}
	var raw, col string
	if err := s.DB.QueryRow(
		`SELECT json, column_name FROM issues WHERE workspace_id = ? AND number = ?`,
		workspaceID, number,
	).Scan(&raw, &col); err != nil {
		return types.Issue{}, err
	}
	var iss types.Issue
	if err := json.Unmarshal([]byte(raw), &iss); err != nil {
		return types.Issue{}, err
	}
	iss.Column = types.BoardColumn(col)
	return iss, nil
}

// SaveIssue upserts an issue row. Preserves the existing column_name and
// manual_order if the row already exists (those are conductor-managed and
// must not be overwritten by a GitHub poll).
//
// Returns (unarchived, err): unarchived is true when an existing archived row
// was just cleared because the incoming issue's state == "open" (#34 q2:
// reopening on GitHub auto-unarchives). The caller should publish
// EvtIssuesArchived so the drawer's (N) badge refreshes.
func (s *Store) SaveIssue(iss types.Issue) (bool, error) {
	if s == nil || s.DB == nil {
		return false, errors.New("store unavailable")
	}
	col := iss.Column
	if col == "" {
		col = types.ColTodo
	}

	// Look up existing column + manual_order so a poll-driven re-save doesn't
	// nuke the user's drag-and-drop placement. Also pull the prior JSON so we
	// can preserve conductor-managed scalars (pr_number / pr_url) that the
	// fresh GitHub fetch knows nothing about. archived_at lives in its own
	// column so we read it directly to decide auto-unarchive on reopen.
	var existingCol sql.NullString
	var existingOrder sql.NullInt64
	var existingJSON sql.NullString
	var existingArchivedAt sql.NullInt64
	row := s.DB.QueryRow(`SELECT column_name, manual_order, json, archived_at FROM issues WHERE workspace_id = ? AND number = ?`, iss.WorkspaceID, iss.Number)
	_ = row.Scan(&existingCol, &existingOrder, &existingJSON, &existingArchivedAt)
	if existingCol.Valid {
		col = types.BoardColumn(existingCol.String)
	}
	if existingJSON.Valid {
		var prev types.Issue
		if err := json.Unmarshal([]byte(existingJSON.String), &prev); err == nil {
			if prev.PRNumber != nil {
				iss.PRNumber = prev.PRNumber
			}
			if prev.PRURL != "" {
				iss.PRURL = prev.PRURL
			}
			// Preserve accumulated work time — GitHub polls don't know about sessions.
			if prev.WorkSeconds > 0 {
				iss.WorkSeconds = prev.WorkSeconds
				iss.WorkSecondsPlan = prev.WorkSecondsPlan
				iss.WorkSecondsExecute = prev.WorkSecondsExecute
			}
		}
	}
	iss.Column = col

	// Decide new archived_at: a state=open save clears it (auto-unarchive on
	// GitHub reopen); anything else preserves the existing value.
	var newArchivedAt any
	unarchived := false
	if iss.State == "open" {
		if existingArchivedAt.Valid {
			unarchived = true
		}
		// newArchivedAt stays nil → SET clears the column.
	} else if existingArchivedAt.Valid {
		newArchivedAt = existingArchivedAt.Int64
	}
	if newArchivedAt != nil {
		t := time.Unix(newArchivedAt.(int64), 0).UTC()
		iss.ArchivedAt = &t
	} else {
		iss.ArchivedAt = nil
	}

	b, err := json.Marshal(iss)
	if err != nil {
		return false, err
	}
	var manualOrder any
	if existingOrder.Valid {
		manualOrder = existingOrder.Int64
	}
	if _, err := s.DB.Exec(`
INSERT INTO issues (workspace_id, number, column_name, manual_order, json, archived_at)
VALUES (?, ?, ?, ?, ?, ?)
ON CONFLICT(workspace_id, number) DO UPDATE SET
    column_name = excluded.column_name,
    manual_order = excluded.manual_order,
    json = excluded.json,
    archived_at = excluded.archived_at`,
		iss.WorkspaceID, iss.Number, string(col), manualOrder, string(b), newArchivedAt); err != nil {
		return false, err
	}
	return unarchived, nil
}

// ListIssues returns all non-archived issues, optionally filtered by workspace.
// Empty workspaceID = all workspaces. Archived rows (#34) are filtered out
// here so Card / Column / Board never see them; use ListArchivedIssues for the
// drawer.
func (s *Store) ListIssues(workspaceID string) ([]types.Issue, error) {
	if s == nil || s.DB == nil {
		return nil, nil
	}
	var (
		rows *sql.Rows
		err  error
	)
	if workspaceID == "" {
		rows, err = s.DB.Query(`SELECT json, column_name, manual_order, archived_at FROM issues WHERE archived_at IS NULL ORDER BY column_name, manual_order, number`)
	} else {
		rows, err = s.DB.Query(`SELECT json, column_name, manual_order, archived_at FROM issues WHERE workspace_id = ? AND archived_at IS NULL ORDER BY column_name, manual_order, number`, workspaceID)
	}
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []types.Issue
	for rows.Next() {
		var raw, col string
		var manual sql.NullInt64
		var archivedAt sql.NullInt64
		if err := rows.Scan(&raw, &col, &manual, &archivedAt); err != nil {
			return nil, err
		}
		var iss types.Issue
		if err := json.Unmarshal([]byte(raw), &iss); err != nil {
			continue
		}
		iss.Column = types.BoardColumn(col)
		if archivedAt.Valid {
			t := time.Unix(archivedAt.Int64, 0).UTC()
			iss.ArchivedAt = &t
		} else {
			iss.ArchivedAt = nil
		}
		out = append(out, iss)
	}
	return out, rows.Err()
}

// ListArchivedIssues returns archived rows (archived_at IS NOT NULL) for the
// given workspace, newest-archived first. Empty workspaceID = all workspaces.
func (s *Store) ListArchivedIssues(workspaceID string) ([]types.Issue, error) {
	if s == nil || s.DB == nil {
		return nil, nil
	}
	var (
		rows *sql.Rows
		err  error
	)
	if workspaceID == "" {
		rows, err = s.DB.Query(`SELECT json, column_name, manual_order, archived_at FROM issues WHERE archived_at IS NOT NULL ORDER BY archived_at DESC, number`)
	} else {
		rows, err = s.DB.Query(`SELECT json, column_name, manual_order, archived_at FROM issues WHERE workspace_id = ? AND archived_at IS NOT NULL ORDER BY archived_at DESC, number`, workspaceID)
	}
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []types.Issue
	for rows.Next() {
		var raw, col string
		var manual sql.NullInt64
		var archivedAt sql.NullInt64
		if err := rows.Scan(&raw, &col, &manual, &archivedAt); err != nil {
			return nil, err
		}
		var iss types.Issue
		if err := json.Unmarshal([]byte(raw), &iss); err != nil {
			continue
		}
		iss.Column = types.BoardColumn(col)
		if archivedAt.Valid {
			t := time.Unix(archivedAt.Int64, 0).UTC()
			iss.ArchivedAt = &t
		}
		out = append(out, iss)
	}
	return out, rows.Err()
}

// ArchiveDone flags every DONE row in the workspace as archived (unix seconds
// now) — bypasses SaveIssue so column_name / manual_order / JSON stay intact.
// Empty workspaceID archives across every workspace (matches the All switcher
// case). Returns the count archived.
func (s *Store) ArchiveDone(workspaceID string) (int, error) {
	if s == nil || s.DB == nil {
		return 0, errors.New("store unavailable")
	}
	res, err := s.DB.Exec(`
UPDATE issues
SET archived_at = strftime('%s','now')
WHERE column_name = 'done' AND archived_at IS NULL
  AND (? = '' OR workspace_id = ?)`,
		workspaceID, workspaceID)
	if err != nil {
		return 0, err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return 0, err
	}
	return int(n), nil
}

// UnarchiveIssue clears archived_at for a single (workspaceID, number) row.
func (s *Store) UnarchiveIssue(workspaceID string, number int) error {
	if s == nil || s.DB == nil {
		return errors.New("store unavailable")
	}
	_, err := s.DB.Exec(`UPDATE issues SET archived_at = NULL WHERE workspace_id = ? AND number = ?`, workspaceID, number)
	return err
}

// UnarchiveAll clears archived_at across every archived row in the workspace.
// Empty workspaceID restores everything across every workspace.
func (s *Store) UnarchiveAll(workspaceID string) error {
	if s == nil || s.DB == nil {
		return errors.New("store unavailable")
	}
	_, err := s.DB.Exec(`UPDATE issues SET archived_at = NULL WHERE archived_at IS NOT NULL AND (? = '' OR workspace_id = ?)`, workspaceID, workspaceID)
	return err
}

// MoveIssueColumn updates the column for an issue and resets manual_order to
// the bottom of the destination column.
func (s *Store) MoveIssueColumn(workspaceID string, number int, column types.BoardColumn) error {
	if s == nil || s.DB == nil {
		return errors.New("store unavailable")
	}
	tx, err := s.DB.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	var maxOrder sql.NullInt64
	if err := tx.QueryRow(
		`SELECT COALESCE(MAX(manual_order), -1) FROM issues WHERE workspace_id = ? AND column_name = ?`,
		workspaceID, string(column),
	).Scan(&maxOrder); err != nil {
		return err
	}
	if _, err := tx.Exec(
		`UPDATE issues SET column_name = ?, manual_order = ? WHERE workspace_id = ? AND number = ?`,
		string(column), maxOrder.Int64+1, workspaceID, number,
	); err != nil {
		return err
	}

	// Round-trip the JSON's column field too so future SaveIssue upserts read
	// the correct value.
	var raw string
	if err := tx.QueryRow(`SELECT json FROM issues WHERE workspace_id = ? AND number = ?`, workspaceID, number).Scan(&raw); err == nil {
		var iss types.Issue
		if err := json.Unmarshal([]byte(raw), &iss); err == nil {
			iss.Column = column
			b, _ := json.Marshal(iss)
			_, _ = tx.Exec(`UPDATE issues SET json = ? WHERE workspace_id = ? AND number = ?`, string(b), workspaceID, number)
		}
	}
	return tx.Commit()
}

// ReorderIssues writes a new manual_order for a sequence of issue numbers
// within a single column (left-to-right or top-to-bottom).
func (s *Store) ReorderIssues(workspaceID string, column types.BoardColumn, ordered []int) error {
	if s == nil || s.DB == nil {
		return errors.New("store unavailable")
	}
	tx, err := s.DB.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for i, num := range ordered {
		if _, err := tx.Exec(
			`UPDATE issues SET manual_order = ?, column_name = ? WHERE workspace_id = ? AND number = ?`,
			i, string(column), workspaceID, num,
		); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// AccumulateIssueWork adds seconds to the issue's work_seconds counters using
// a read-modify-write transaction. Called when a plan or execute session ends
// so the card shows cumulative active work time (issue #46).
func (s *Store) AccumulateIssueWork(workspaceID string, number int, mode types.SessionMode, seconds int64) error {
	if s == nil || s.DB == nil {
		return errors.New("store unavailable")
	}
	if seconds <= 0 {
		return nil
	}
	tx, err := s.DB.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	var raw string
	if err := tx.QueryRow(`SELECT json FROM issues WHERE workspace_id = ? AND number = ?`, workspaceID, number).Scan(&raw); err != nil {
		return err
	}
	var iss types.Issue
	if err := json.Unmarshal([]byte(raw), &iss); err != nil {
		return err
	}
	iss.WorkSeconds += seconds
	switch mode {
	case types.ModePlan:
		iss.WorkSecondsPlan += seconds
	case types.ModeExecute:
		iss.WorkSecondsExecute += seconds
	}
	b, err := json.Marshal(iss)
	if err != nil {
		return err
	}
	if _, err := tx.Exec(`UPDATE issues SET json = ? WHERE workspace_id = ? AND number = ?`, string(b), workspaceID, number); err != nil {
		return err
	}
	return tx.Commit()
}

// RemoveIssue deletes a row.
func (s *Store) RemoveIssue(workspaceID string, number int) error {
	if s == nil || s.DB == nil {
		return errors.New("store unavailable")
	}
	_, err := s.DB.Exec(`DELETE FROM issues WHERE workspace_id = ? AND number = ?`, workspaceID, number)
	return err
}

// ReconcileClosedIssues routes any issue that's already state=closed in its
// JSON but stuck in a non-done column over to DONE. Self-healing pass for
// rows left half-migrated by an earlier buggy poll. Returns the number of
// rows fixed.
func (s *Store) ReconcileClosedIssues() (int, error) {
	if s == nil || s.DB == nil {
		return 0, errors.New("store unavailable")
	}
	rows, err := s.DB.Query(`SELECT workspace_id, number, json FROM issues WHERE column_name != 'done'`)
	if err != nil {
		return 0, err
	}
	type pending struct {
		ws  string
		num int
	}
	var todo []pending
	for rows.Next() {
		var ws, raw string
		var num int
		if err := rows.Scan(&ws, &num, &raw); err != nil {
			rows.Close()
			return 0, err
		}
		var iss types.Issue
		if err := json.Unmarshal([]byte(raw), &iss); err != nil {
			continue
		}
		if iss.State == "closed" {
			todo = append(todo, pending{ws: ws, num: num})
		}
	}
	rows.Close()
	for _, p := range todo {
		if err := s.MarkIssueClosed(p.ws, p.num); err != nil {
			return 0, err
		}
	}
	return len(todo), nil
}

// MarkPROpened persists the PR number + URL on an issue and moves the card
// to REVIEW (bottom of column). Single transaction; bypasses SaveIssue's
// column-preservation logic so the IN_PROGRESS→REVIEW jump survives the next
// poll tick. Modeled on MarkIssueClosed.
func (s *Store) MarkPROpened(workspaceID string, number int, prNumber int, prURL string) error {
	if s == nil || s.DB == nil {
		return errors.New("store unavailable")
	}
	tx, err := s.DB.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	var raw string
	if err := tx.QueryRow(`SELECT json FROM issues WHERE workspace_id = ? AND number = ?`,
		workspaceID, number).Scan(&raw); err != nil {
		return err
	}
	var iss types.Issue
	if err := json.Unmarshal([]byte(raw), &iss); err != nil {
		return err
	}
	iss.PRNumber = &prNumber
	iss.PRURL = prURL
	iss.Column = types.ColReview
	b, _ := json.Marshal(iss)

	var maxOrder sql.NullInt64
	if err := tx.QueryRow(
		`SELECT COALESCE(MAX(manual_order), -1) FROM issues WHERE workspace_id = ? AND column_name = ?`,
		workspaceID, string(types.ColReview),
	).Scan(&maxOrder); err != nil {
		return err
	}
	if _, err := tx.Exec(
		`UPDATE issues SET column_name = ?, manual_order = ?, json = ? WHERE workspace_id = ? AND number = ?`,
		string(types.ColReview), maxOrder.Int64+1, string(b), workspaceID, number,
	); err != nil {
		return err
	}
	return tx.Commit()
}

// MarkPRMerged moves the card to DONE and marks the issue closed, mirroring
// MarkIssueClosed but driven by the GitHub PR-state probe (#33). Preserves
// pr_number / pr_url so the chip stays on the DONE card as history. Clears
// last_error in the same tx so a stale failure string doesn't linger.
// Idempotent: re-runs are no-ops because the column is already DONE.
func (s *Store) MarkPRMerged(workspaceID string, number int) error {
	if s == nil || s.DB == nil {
		return errors.New("store unavailable")
	}
	tx, err := s.DB.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	var raw string
	if err := tx.QueryRow(`SELECT json FROM issues WHERE workspace_id = ? AND number = ?`,
		workspaceID, number).Scan(&raw); err != nil {
		return err
	}
	var iss types.Issue
	if err := json.Unmarshal([]byte(raw), &iss); err != nil {
		return err
	}
	iss.State = "closed"
	iss.Column = types.ColDone
	iss.LastError = ""
	b, _ := json.Marshal(iss)

	var maxOrder sql.NullInt64
	if err := tx.QueryRow(
		`SELECT COALESCE(MAX(manual_order), -1) FROM issues WHERE workspace_id = ? AND column_name = ?`,
		workspaceID, string(types.ColDone),
	).Scan(&maxOrder); err != nil {
		return err
	}
	if _, err := tx.Exec(
		`UPDATE issues SET column_name = ?, manual_order = ?, json = ? WHERE workspace_id = ? AND number = ?`,
		string(types.ColDone), maxOrder.Int64+1, string(b), workspaceID, number,
	); err != nil {
		return err
	}
	return tx.Commit()
}

// MarkPRClosedUnmerged clears pr_number / pr_url on an issue whose PR was
// closed without merging (#33). Leaves column_name / manual_order alone —
// per rev2 q2, trust the user's manual placement. Bypasses SaveIssue because
// SaveIssue's preserve-PR-fields path would resurrect the cleared values on
// the next poll.
func (s *Store) MarkPRClosedUnmerged(workspaceID string, number int) error {
	if s == nil || s.DB == nil {
		return errors.New("store unavailable")
	}
	tx, err := s.DB.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	var raw string
	if err := tx.QueryRow(`SELECT json FROM issues WHERE workspace_id = ? AND number = ?`,
		workspaceID, number).Scan(&raw); err != nil {
		return err
	}
	var iss types.Issue
	if err := json.Unmarshal([]byte(raw), &iss); err != nil {
		return err
	}
	iss.PRNumber = nil
	iss.PRURL = ""
	b, _ := json.Marshal(iss)
	if _, err := tx.Exec(
		`UPDATE issues SET json = ? WHERE workspace_id = ? AND number = ?`,
		string(b), workspaceID, number,
	); err != nil {
		return err
	}
	return tx.Commit()
}

// MarkIssueClosed forces state=closed AND column=done in a single transaction,
// bypassing SaveIssue's column-preservation logic (which would otherwise keep
// the closed issue stuck in whatever column the user last placed it in).
// Called by the GitHub poller when an issue is no longer in the open list.
func (s *Store) MarkIssueClosed(workspaceID string, number int) error {
	if s == nil || s.DB == nil {
		return errors.New("store unavailable")
	}
	tx, err := s.DB.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	var raw string
	if err := tx.QueryRow(`SELECT json FROM issues WHERE workspace_id = ? AND number = ?`,
		workspaceID, number).Scan(&raw); err != nil {
		return err
	}
	var iss types.Issue
	if err := json.Unmarshal([]byte(raw), &iss); err != nil {
		return err
	}
	iss.State = "closed"
	iss.Column = types.ColDone
	b, _ := json.Marshal(iss)
	if _, err := tx.Exec(
		`UPDATE issues SET column_name = ?, json = ? WHERE workspace_id = ? AND number = ?`,
		string(types.ColDone), string(b), workspaceID, number,
	); err != nil {
		return err
	}
	return tx.Commit()
}
