package store

import (
	"database/sql"
	"errors"
	"time"

	"prismconductor/internal/store/jsonutil"
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
	if _, err := jsonutil.Load([]byte(raw), &iss); err != nil {
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
	var existingClosedAt sql.NullInt64
	row := s.DB.QueryRow(`SELECT column_name, manual_order, json, archived_at, closed_at FROM issues WHERE workspace_id = ? AND number = ?`, iss.WorkspaceID, iss.Number)
	_ = row.Scan(&existingCol, &existingOrder, &existingJSON, &existingArchivedAt, &existingClosedAt)
	if existingCol.Valid {
		col = types.BoardColumn(existingCol.String)
	}
	var rawMap map[string]any
	if existingJSON.Valid {
		var prev types.Issue
		var err error
		rawMap, err = jsonutil.Load([]byte(existingJSON.String), &prev)
		if err == nil {
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
			// Preserve accumulated LLM cost (issue #47).
			if prev.CostUSD > 0 {
				iss.CostUSD = prev.CostUSD
			}
			// Preserve WaitingForPool — this is purely conductor-managed
			// state (set when a spawn was queued because no pool slot was
			// free; cleared when the orchestrator drains the queue and
			// successfully spawns). The GitHub poll has no opinion on it,
			// so any incoming SaveIssue from the poller would otherwise
			// clobber waiting_for_pool=true back to false on the next 5-min
			// tick — meaning the card's pink "waiting for available agent
			// pool" decoration silently disappears even though the
			// pending_pool_for row is still queued. orchestrator's
			// clearWaitingForPool is the only call site that should ever
			// flip this back to false.
			if prev.WaitingForPool {
				iss.WaitingForPool = true
			}
			// Preserve NeedsPRInfo: a GitHub poll has no opinion on whether
			// the push succeeded. MarkPROpened is the only caller that clears it.
			if prev.NeedsPRInfo != nil {
				iss.NeedsPRInfo = prev.NeedsPRInfo
			}
		}
	}
	iss.Column = col

	// archived_at is STICKY across SaveIssue calls. The original design tried
	// to auto-unarchive on every save where State=="open" (so that a GitHub
	// reopen would restore the card to the board), but the GitHub poller
	// calls SaveIssue every 5 minutes for every issue — including DONE-column
	// cards whose GitHub issue is still open. The result was that "Archive
	// Done" worked for a few minutes and then the next poll cycle re-cleared
	// archived_at, dumping every archived card back into DONE. Make
	// archive/unarchive purely user-driven: SaveIssue never mutates
	// archived_at. The explicit UnarchiveIssue / UnarchiveAll paths are the
	// only way to restore an archived row.
	var newArchivedAt any
	unarchived := false
	if existingArchivedAt.Valid {
		newArchivedAt = existingArchivedAt.Int64
	}
	if newArchivedAt != nil {
		t := time.Unix(newArchivedAt.(int64), 0).UTC()
		iss.ArchivedAt = &t
	} else {
		iss.ArchivedAt = nil
	}

	// closed_at is also STICKY: a GitHub poll doesn't carry this; only
	// MarkIssueClosed / MarkPRMerged set it. Preserve whatever's in the DB.
	var newClosedAt any
	if existingClosedAt.Valid {
		newClosedAt = existingClosedAt.Int64
		t := time.Unix(existingClosedAt.Int64, 0).UTC()
		iss.ClosedAt = &t
	} else {
		iss.ClosedAt = nil
	}

	b, err := jsonutil.Save(rawMap, iss)
	if err != nil {
		return false, err
	}
	var manualOrder any
	if existingOrder.Valid {
		manualOrder = existingOrder.Int64
	}
	if _, err := s.DB.Exec(`
INSERT INTO issues (workspace_id, number, column_name, manual_order, json, archived_at, closed_at)
VALUES (?, ?, ?, ?, ?, ?, ?)
ON CONFLICT(workspace_id, number) DO UPDATE SET
    column_name = excluded.column_name,
    manual_order = excluded.manual_order,
    json = excluded.json,
    archived_at = excluded.archived_at,
    closed_at = excluded.closed_at`,
		iss.WorkspaceID, iss.Number, string(col), manualOrder, string(b), newArchivedAt, newClosedAt); err != nil {
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
		if _, err := jsonutil.Load([]byte(raw), &iss); err != nil {
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
		if _, err := jsonutil.Load([]byte(raw), &iss); err != nil {
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

// SetIssueWaitingForPool atomically writes `$.waiting_for_pool` inside the
// JSON blob via json_set, bypassing SaveIssue's preservation logic.
//
// Why this exists as a dedicated method: SaveIssue preserves
// `waiting_for_pool=true` across re-saves (see issues.go:96 — needed so the
// GitHub poller's 5-min SaveIssue cycle doesn't clobber the conductor's
// queued-spawn flag back to false). That preservation rule is correct for
// the poll case but BLOCKS the legitimate clear path: orchestrator's
// clearWaitingForPool would Load → set false → SaveIssue, but SaveIssue
// re-reads prev (still true), preservation kicks in, and the flag is forced
// back to true. The flag could never actually be cleared.
//
// json_set avoids the read-modify-write entirely; the indexed column-style
// fix that worked for UpdateSessionState (#sessions.go:UpdateSessionState).
//
// Use only from paths that should authoritatively set/clear the flag
// (orchestrator drain success, explicit user action). NOT from anywhere
// that should respect the GitHub-poll preservation rule.
func (s *Store) SetIssueWaitingForPool(workspaceID string, number int, waiting bool) error {
	if s == nil || s.DB == nil {
		return errors.New("store unavailable")
	}
	v := "false"
	if waiting {
		v = "true"
	}
	_, err := s.DB.Exec(
		`UPDATE issues
		    SET json = json_set(json, '$.waiting_for_pool', json(?))
		  WHERE workspace_id = ? AND number = ?`,
		v, workspaceID, number)
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
		if rawMap, err := jsonutil.Load([]byte(raw), &iss); err == nil {
			iss.Column = column
			b, _ := jsonutil.Save(rawMap, iss)
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
	rawMap, err := jsonutil.Load([]byte(raw), &iss)
	if err != nil {
		return err
	}
	iss.WorkSeconds += seconds
	switch mode {
	case types.ModePlan:
		iss.WorkSecondsPlan += seconds
	case types.ModeExecute:
		iss.WorkSecondsExecute += seconds
	}
	b, err := jsonutil.Save(rawMap, iss)
	if err != nil {
		return err
	}
	if _, err := tx.Exec(`UPDATE issues SET json = ? WHERE workspace_id = ? AND number = ?`, string(b), workspaceID, number); err != nil {
		return err
	}
	return tx.Commit()
}

// AccumulateIssueCost adds LLM token counts and dollar cost to the issue's
// cost_usd field (issue #47). Called when a session ends and its result event
// carried total_cost_usd data. Zero costUSD is a no-op.
func (s *Store) AccumulateIssueCost(workspaceID string, number int, costUSD float64) error {
	if s == nil || s.DB == nil {
		return errors.New("store unavailable")
	}
	if costUSD <= 0 {
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
	rawMap, err := jsonutil.Load([]byte(raw), &iss)
	if err != nil {
		return err
	}
	iss.CostUSD += costUSD
	b, err := jsonutil.Save(rawMap, iss)
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

// ReconcileStalePoolQueueForClosedIssues clears `waiting_for_pool` and
// removes `pending_pool_for` rows for any issue whose card is in REVIEW
// (PR open) or DONE (PR merged/closed). A queued spawn for an issue
// whose work has already shipped (or whose PR was rejected) is by
// definition moot; leaving the row + flag around makes the card glow
// pink "waiting for available agent pool" forever in DONE. Witnessed
// live on #118 (DONE column, PR #166 closed/merged, but
// waiting_for_pool=1 + a stale pending_pool_for row from the original
// approve-execute enqueue).
//
// Returns (issuesFixed, queueRowsDeleted).
func (s *Store) ReconcileStalePoolQueueForClosedIssues() (int, int, error) {
	if s == nil || s.DB == nil {
		return 0, 0, errors.New("store unavailable")
	}
	res1, err := s.DB.Exec(`
UPDATE issues
   SET json = json_set(json, '$.waiting_for_pool', json('false'))
 WHERE column_name IN ('review','done')
   AND json_extract(json, '$.waiting_for_pool') = 1`)
	if err != nil {
		return 0, 0, err
	}
	issuesFixed, _ := res1.RowsAffected()
	res2, err := s.DB.Exec(`
DELETE FROM pending_pool_for
 WHERE EXISTS (
   SELECT 1 FROM issues
    WHERE issues.workspace_id = pending_pool_for.workspace_id
      AND issues.number       = pending_pool_for.issue_number
      AND issues.column_name IN ('review','done')
 )`)
	if err != nil {
		return int(issuesFixed), 0, err
	}
	queueRowsDeleted, _ := res2.RowsAffected()
	return int(issuesFixed), int(queueRowsDeleted), nil
}

// ReconcileStaleRunningSessions marks any session row whose state is still
// `running` or `waiting_for_input` but whose corresponding issue is in
// REVIEW (PR open) or DONE (PR merged) as failed. Self-healing pass for
// harness sessions whose goroutine died/hung mid-run without updating its
// DB row, leaving the IssueView assembler picking the stale row as
// `active_session` and Card.tsx rendering a glow on a card whose work has
// demonstrably shipped. Witnessed on issue #109. Returns the number of
// rows reconciled.
func (s *Store) ReconcileStaleRunningSessions() (int, error) {
	if s == nil || s.DB == nil {
		return 0, errors.New("store unavailable")
	}
	res, err := s.DB.Exec(`
UPDATE sessions
   SET state = 'failed',
       json  = json_set(json,
                 '$.state', 'failed',
                 '$.blocked_reason', 'reconciled at startup: session left state=running while issue is in review/done')
 WHERE state IN ('running','waiting_for_input')
   AND EXISTS (
       SELECT 1
         FROM issues
        WHERE issues.workspace_id = sessions.workspace_id
          AND issues.number       = sessions.issue_number
          AND issues.column_name IN ('review','done')
   )`)
	if err != nil {
		return 0, err
	}
	n, _ := res.RowsAffected()
	return int(n), nil
}

// ReconcileClosedIssues routes any issue that's already state=closed in its
// JSON but stuck in a non-done column over to DONE. Self-healing pass for
// rows left half-migrated by an earlier buggy poll. Returns the number of
// rows fixed.
func (s *Store) ReconcileClosedIssues() (int, error) {
	if s == nil || s.DB == nil {
		return 0, errors.New("store unavailable")
	}
	rows, err := s.DB.Query(`SELECT workspace_id, number, json FROM issues WHERE column_name != 'done' AND archived_at IS NULL`)
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
		if _, err := jsonutil.Load([]byte(raw), &iss); err != nil {
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
	rawMap, err := jsonutil.Load([]byte(raw), &iss)
	if err != nil {
		return err
	}
	iss.PRNumber = &prNumber
	iss.PRURL = prURL
	iss.Column = types.ColReview
	iss.NeedsPRInfo = nil // clear the NEEDS_PR badge now that a PR is attached
	b, _ := jsonutil.Save(rawMap, iss)

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

// MarkNeedsPR sets NeedsPRInfo on the issue and moves the card to REVIEW (#157).
// Called when an execute session emits the NEEDS_PR: sentinel (code committed
// locally, push failed). MarkPROpened clears the NeedsPRInfo once a PR appears.
func (s *Store) MarkNeedsPR(workspaceID string, number int, info types.NeedsPRInfo) error {
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
	rawMap, err := jsonutil.Load([]byte(raw), &iss)
	if err != nil {
		return err
	}
	iss.NeedsPRInfo = &info
	iss.Column = types.ColReview
	b, _ := jsonutil.Save(rawMap, iss)

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
	var existingClosedAt sql.NullInt64
	if err := tx.QueryRow(`SELECT json, closed_at FROM issues WHERE workspace_id = ? AND number = ?`,
		workspaceID, number).Scan(&raw, &existingClosedAt); err != nil {
		return err
	}
	var iss types.Issue
	rawMap, err := jsonutil.Load([]byte(raw), &iss)
	if err != nil {
		return err
	}
	iss.State = "closed"
	iss.Column = types.ColDone
	iss.LastError = ""
	// PR is merged → any queued pool spawn for this issue is now moot. Clear
	// the conductor-owned waiting_for_pool flag in the JSON we're about to
	// write (avoids the SaveIssue preservation rule by going through the
	// raw blob marshal path). The matching pending_pool_for row gets
	// deleted below in the same transaction.
	iss.WaitingForPool = false

	var newClosedAt any
	if existingClosedAt.Valid {
		newClosedAt = existingClosedAt.Int64
		t := time.Unix(existingClosedAt.Int64, 0).UTC()
		iss.ClosedAt = &t
	} else {
		now := time.Now().UTC()
		newClosedAt = now.Unix()
		iss.ClosedAt = &now
	}

	b, _ := jsonutil.Save(rawMap, iss)

	var maxOrder sql.NullInt64
	if err := tx.QueryRow(
		`SELECT COALESCE(MAX(manual_order), -1) FROM issues WHERE workspace_id = ? AND column_name = ?`,
		workspaceID, string(types.ColDone),
	).Scan(&maxOrder); err != nil {
		return err
	}
	if _, err := tx.Exec(
		`UPDATE issues SET column_name = ?, manual_order = ?, json = ?, closed_at = ? WHERE workspace_id = ? AND number = ?`,
		string(types.ColDone), maxOrder.Int64+1, string(b), newClosedAt, workspaceID, number,
	); err != nil {
		return err
	}
	// Reap any leftover pool-queue rows. Witnessed leak: PR for #118
	// merged, card moved to DONE, but a stale `pending_pool_for` row from
	// the original approve-execute enqueue stuck around AND
	// json.waiting_for_pool stayed true → card glowed pink in DONE forever.
	if _, err := tx.Exec(
		`DELETE FROM pending_pool_for WHERE workspace_id = ? AND issue_number = ?`,
		workspaceID, number,
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
	rawMap, err := jsonutil.Load([]byte(raw), &iss)
	if err != nil {
		return err
	}
	iss.PRNumber = nil
	iss.PRURL = ""
	// PR closed without merging → any queued pool spawn is moot. Same
	// reaping as MarkPRMerged.
	iss.WaitingForPool = false
	b, _ := jsonutil.Save(rawMap, iss)
	if _, err := tx.Exec(
		`UPDATE issues SET json = ? WHERE workspace_id = ? AND number = ?`,
		string(b), workspaceID, number,
	); err != nil {
		return err
	}
	if _, err := tx.Exec(
		`DELETE FROM pending_pool_for WHERE workspace_id = ? AND issue_number = ?`,
		workspaceID, number,
	); err != nil {
		return err
	}
	return tx.Commit()
}

// MarkIssueClosed forces state=closed AND column=done in a single transaction,
// bypassing SaveIssue's column-preservation logic (which would otherwise keep
// the closed issue stuck in whatever column the user last placed it in).
// Called by the GitHub poller when an issue is no longer in the open list.
// Skips archived cards so archive state is never disturbed by polling.
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
	var existingArchivedAt sql.NullInt64
	var existingClosedAt sql.NullInt64
	if err := tx.QueryRow(`SELECT json, archived_at, closed_at FROM issues WHERE workspace_id = ? AND number = ?`,
		workspaceID, number).Scan(&raw, &existingArchivedAt, &existingClosedAt); err != nil {
		return err
	}
	// Don't touch archived cards — the next poll cycle must not resurrect them.
	if existingArchivedAt.Valid {
		return nil
	}
	var iss types.Issue
	rawMap, err := jsonutil.Load([]byte(raw), &iss)
	if err != nil {
		return err
	}
	iss.State = "closed"
	iss.Column = types.ColDone

	// Record first-close time for ArchiveClosedByAge.
	var newClosedAt any
	if existingClosedAt.Valid {
		newClosedAt = existingClosedAt.Int64
		t := time.Unix(existingClosedAt.Int64, 0).UTC()
		iss.ClosedAt = &t
	} else {
		now := time.Now().UTC()
		newClosedAt = now.Unix()
		iss.ClosedAt = &now
	}

	b, _ := jsonutil.Save(rawMap, iss)
	if _, err := tx.Exec(
		`UPDATE issues SET column_name = ?, json = ?, closed_at = ? WHERE workspace_id = ? AND number = ?`,
		string(types.ColDone), string(b), newClosedAt, workspaceID, number,
	); err != nil {
		return err
	}
	return tx.Commit()
}

// ArchiveClosedByAge archives DONE cards in the given workspace whose
// closed_at timestamp is at least daysClosed days ago. Empty workspaceID
// applies across all workspaces. Returns the number of rows archived.
// Called by the auto-archive engine (issue #107).
func (s *Store) ArchiveClosedByAge(workspaceID string, daysClosed int) (int, error) {
	if s == nil || s.DB == nil {
		return 0, errors.New("store unavailable")
	}
	if daysClosed < 1 {
		daysClosed = 1
	}
	cutoff := time.Now().UTC().Add(-time.Duration(daysClosed) * 24 * time.Hour).Unix()
	res, err := s.DB.Exec(`
UPDATE issues
SET archived_at = strftime('%s','now')
WHERE column_name = 'done'
  AND archived_at IS NULL
  AND closed_at IS NOT NULL
  AND closed_at <= ?
  AND (? = '' OR workspace_id = ?)`,
		cutoff, workspaceID, workspaceID)
	if err != nil {
		return 0, err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return 0, err
	}
	return int(n), nil
}
