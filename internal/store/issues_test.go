package store

import (
	"testing"
	"time"

	"prismconductor/internal/types"
)

func newTestStore(t *testing.T) *Store {
	t.Helper()
	s, err := Open(t.TempDir())
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return s
}

func TestMarkPROpenedMovesCardAndPersistsFields(t *testing.T) {
	s := newTestStore(t)
	if _, err := s.SaveIssue(types.Issue{
		WorkspaceID: "ws1",
		Number:      42,
		Title:       "thing",
		State:       "open",
		Column:      types.ColInProgress,
	}); err != nil {
		t.Fatalf("SaveIssue: %v", err)
	}

	if err := s.MarkPROpened("ws1", 42, 99, "https://github.com/o/r/pull/99"); err != nil {
		t.Fatalf("MarkPROpened: %v", err)
	}

	got, err := s.ListIssues("ws1")
	if err != nil || len(got) != 1 {
		t.Fatalf("ListIssues: %v len=%d", err, len(got))
	}
	iss := got[0]
	if iss.Column != types.ColReview {
		t.Errorf("column = %q, want %q", iss.Column, types.ColReview)
	}
	if iss.PRNumber == nil || *iss.PRNumber != 99 {
		t.Errorf("PRNumber = %v, want 99", iss.PRNumber)
	}
	if iss.PRURL != "https://github.com/o/r/pull/99" {
		t.Errorf("PRURL = %q", iss.PRURL)
	}
}

func TestMarkPRMergedMovesCardToDoneClearsLastErrorKeepsPRFields(t *testing.T) {
	s := newTestStore(t)
	prNum := 99
	if _, err := s.SaveIssue(types.Issue{
		WorkspaceID: "ws1",
		Number:      42,
		Title:       "thing",
		State:       "open",
		Column:      types.ColReview,
		PRNumber:    &prNum,
		PRURL:       "https://github.com/o/r/pull/99",
		LastError:   "stale failure from a prior run",
	}); err != nil {
		t.Fatalf("SaveIssue: %v", err)
	}

	if err := s.MarkPRMerged("ws1", 42); err != nil {
		t.Fatalf("MarkPRMerged: %v", err)
	}

	got, err := s.ListIssues("ws1")
	if err != nil || len(got) != 1 {
		t.Fatalf("ListIssues: %v len=%d", err, len(got))
	}
	iss := got[0]
	if iss.Column != types.ColDone {
		t.Errorf("column = %q, want %q", iss.Column, types.ColDone)
	}
	if iss.State != "closed" {
		t.Errorf("state = %q, want closed", iss.State)
	}
	if iss.LastError != "" {
		t.Errorf("LastError = %q, want empty", iss.LastError)
	}
	if iss.PRNumber == nil || *iss.PRNumber != 99 {
		t.Errorf("PRNumber = %v, want 99 (kept as history)", iss.PRNumber)
	}
	if iss.PRURL != "https://github.com/o/r/pull/99" {
		t.Errorf("PRURL = %q, want kept as history", iss.PRURL)
	}
}

func TestMarkPRClosedUnmergedClearsPRFieldsAndPreservesColumn(t *testing.T) {
	s := newTestStore(t)
	prNum := 12
	if _, err := s.SaveIssue(types.Issue{
		WorkspaceID: "ws1",
		Number:      7,
		Title:       "thing",
		State:       "open",
		Column:      types.ColReview,
		PRNumber:    &prNum,
		PRURL:       "https://github.com/o/r/pull/12",
	}); err != nil {
		t.Fatalf("SaveIssue: %v", err)
	}

	if err := s.MarkPRClosedUnmerged("ws1", 7); err != nil {
		t.Fatalf("MarkPRClosedUnmerged: %v", err)
	}

	got, err := s.ListIssues("ws1")
	if err != nil || len(got) != 1 {
		t.Fatalf("ListIssues: %v len=%d", err, len(got))
	}
	iss := got[0]
	if iss.PRNumber != nil {
		t.Errorf("PRNumber = %v, want nil", iss.PRNumber)
	}
	if iss.PRURL != "" {
		t.Errorf("PRURL = %q, want empty", iss.PRURL)
	}
	if iss.Column != types.ColReview {
		t.Errorf("column = %q, want %q (column must be preserved)", iss.Column, types.ColReview)
	}
}

// Regression: SaveIssue's preserve-PR-fields path at issues.go:57-67 would
// resurrect the cleared values after the next poll. Ensure the cleared state
// survives a subsequent SaveIssue call with no PR fields on the fresh row.
func TestMarkPRClosedUnmergedSurvivesNextPoll(t *testing.T) {
	s := newTestStore(t)
	prNum := 12
	if _, err := s.SaveIssue(types.Issue{
		WorkspaceID: "ws1",
		Number:      7,
		Title:       "thing",
		State:       "open",
		Column:      types.ColReview,
		PRNumber:    &prNum,
		PRURL:       "https://github.com/o/r/pull/12",
	}); err != nil {
		t.Fatalf("SaveIssue: %v", err)
	}
	if err := s.MarkPRClosedUnmerged("ws1", 7); err != nil {
		t.Fatalf("MarkPRClosedUnmerged: %v", err)
	}
	if _, err := s.SaveIssue(types.Issue{
		WorkspaceID: "ws1",
		Number:      7,
		Title:       "thing (refreshed)",
		State:       "open",
	}); err != nil {
		t.Fatalf("SaveIssue refresh: %v", err)
	}
	got, err := s.ListIssues("ws1")
	if err != nil || len(got) != 1 {
		t.Fatalf("ListIssues: %v len=%d", err, len(got))
	}
	if got[0].PRNumber != nil {
		t.Errorf("PRNumber resurrected by SaveIssue: %v", got[0].PRNumber)
	}
	if got[0].PRURL != "" {
		t.Errorf("PRURL resurrected by SaveIssue: %q", got[0].PRURL)
	}
}

func TestSaveIssuePreservesPRFieldsAcrossPolls(t *testing.T) {
	s := newTestStore(t)
	if _, err := s.SaveIssue(types.Issue{
		WorkspaceID: "ws1",
		Number:      7,
		Title:       "polled",
		State:       "open",
		Column:      types.ColInProgress,
	}); err != nil {
		t.Fatalf("SaveIssue initial: %v", err)
	}
	// Worker writes PR fields via MarkPROpened.
	if err := s.MarkPROpened("ws1", 7, 12, "https://github.com/o/r/pull/12"); err != nil {
		t.Fatalf("MarkPROpened: %v", err)
	}
	// Simulate a poll-driven re-save where GitHub knows nothing about pr_number / pr_url.
	if _, err := s.SaveIssue(types.Issue{
		WorkspaceID: "ws1",
		Number:      7,
		Title:       "polled (refreshed)",
		State:       "open",
	}); err != nil {
		t.Fatalf("SaveIssue refresh: %v", err)
	}
	got, err := s.ListIssues("ws1")
	if err != nil || len(got) != 1 {
		t.Fatalf("ListIssues: %v len=%d", err, len(got))
	}
	iss := got[0]
	if iss.Title != "polled (refreshed)" {
		t.Errorf("title not refreshed: %q", iss.Title)
	}
	if iss.PRNumber == nil || *iss.PRNumber != 12 {
		t.Errorf("PRNumber lost across poll: %v", iss.PRNumber)
	}
	if iss.PRURL != "https://github.com/o/r/pull/12" {
		t.Errorf("PRURL lost across poll: %q", iss.PRURL)
	}
	if iss.Column != types.ColReview {
		t.Errorf("column lost across poll: %q", iss.Column)
	}
}

// --- Archive (#34) ---

func seedDone(t *testing.T, s *Store, ws string, num int) {
	t.Helper()
	if _, err := s.SaveIssue(types.Issue{
		WorkspaceID: ws,
		Number:      num,
		Title:       "done card",
		State:       "closed",
		Column:      types.ColDone,
	}); err != nil {
		t.Fatalf("seed SaveIssue ws=%s #%d: %v", ws, num, err)
	}
}

func TestArchiveDoneScopedToWorkspace(t *testing.T) {
	s := newTestStore(t)
	seedDone(t, s, "ws1", 1)
	seedDone(t, s, "ws1", 2)
	seedDone(t, s, "ws2", 3)

	n, err := s.ArchiveDone("ws1")
	if err != nil {
		t.Fatalf("ArchiveDone: %v", err)
	}
	if n != 2 {
		t.Errorf("archived count = %d, want 2", n)
	}

	live1, _ := s.ListIssues("ws1")
	if len(live1) != 0 {
		t.Errorf("ws1 still shows %d non-archived rows, want 0", len(live1))
	}
	live2, _ := s.ListIssues("ws2")
	if len(live2) != 1 || live2[0].Number != 3 {
		t.Errorf("ws2 should be untouched: got %+v", live2)
	}
	arch1, _ := s.ListArchivedIssues("ws1")
	if len(arch1) != 2 {
		t.Errorf("ws1 archived = %d, want 2", len(arch1))
	}
	for _, iss := range arch1 {
		if iss.ArchivedAt == nil {
			t.Errorf("archived row #%d missing ArchivedAt", iss.Number)
		}
	}
}

func TestArchiveDoneEmptyWorkspaceArchivesAll(t *testing.T) {
	s := newTestStore(t)
	seedDone(t, s, "ws1", 1)
	seedDone(t, s, "ws2", 2)

	n, err := s.ArchiveDone("")
	if err != nil {
		t.Fatalf("ArchiveDone: %v", err)
	}
	if n != 2 {
		t.Errorf("archived count = %d, want 2", n)
	}
	all, _ := s.ListIssues("")
	if len(all) != 0 {
		t.Errorf("expected zero non-archived rows after global archive, got %d", len(all))
	}
}

func TestArchiveDoneSkipsNonDoneColumns(t *testing.T) {
	s := newTestStore(t)
	cols := []types.BoardColumn{types.ColTodo, types.ColPlan, types.ColInProgress, types.ColReview}
	for i, c := range cols {
		if _, err := s.SaveIssue(types.Issue{
			WorkspaceID: "ws1",
			Number:      i + 1,
			Title:       "x",
			State:       "open",
			Column:      c,
		}); err != nil {
			t.Fatalf("seed: %v", err)
		}
	}
	n, err := s.ArchiveDone("ws1")
	if err != nil {
		t.Fatalf("ArchiveDone: %v", err)
	}
	if n != 0 {
		t.Errorf("archived count = %d, want 0", n)
	}
	live, _ := s.ListIssues("ws1")
	if len(live) != len(cols) {
		t.Errorf("non-DONE rows lost: got %d, want %d", len(live), len(cols))
	}
}

func TestListIssuesHidesArchived(t *testing.T) {
	s := newTestStore(t)
	seedDone(t, s, "ws1", 1)
	seedDone(t, s, "ws1", 2)
	if _, err := s.ArchiveDone("ws1"); err != nil {
		t.Fatalf("ArchiveDone: %v", err)
	}
	// Add a fresh non-archived row.
	if _, err := s.SaveIssue(types.Issue{
		WorkspaceID: "ws1",
		Number:      3,
		Title:       "fresh",
		State:       "open",
		Column:      types.ColTodo,
	}); err != nil {
		t.Fatalf("SaveIssue: %v", err)
	}
	live, _ := s.ListIssues("ws1")
	if len(live) != 1 || live[0].Number != 3 {
		t.Errorf("ListIssues should return only #3, got %+v", live)
	}
}

func TestListArchivedIssuesReturnsArchivedDesc(t *testing.T) {
	s := newTestStore(t)
	seedDone(t, s, "ws1", 1)
	seedDone(t, s, "ws1", 2)
	seedDone(t, s, "ws1", 3)
	// Archive in 3 staggered batches via direct UPDATE so ordering is testable.
	if _, err := s.DB.Exec(`UPDATE issues SET archived_at = 100 WHERE number = 1`); err != nil {
		t.Fatal(err)
	}
	if _, err := s.DB.Exec(`UPDATE issues SET archived_at = 300 WHERE number = 2`); err != nil {
		t.Fatal(err)
	}
	if _, err := s.DB.Exec(`UPDATE issues SET archived_at = 200 WHERE number = 3`); err != nil {
		t.Fatal(err)
	}
	arch, err := s.ListArchivedIssues("ws1")
	if err != nil {
		t.Fatalf("ListArchivedIssues: %v", err)
	}
	if len(arch) != 3 {
		t.Fatalf("len = %d, want 3", len(arch))
	}
	want := []int{2, 3, 1}
	for i, iss := range arch {
		if iss.Number != want[i] {
			t.Errorf("ordering[%d] = #%d, want #%d", i, iss.Number, want[i])
		}
	}
}

func TestUnarchiveIssueRestoresVisibility(t *testing.T) {
	s := newTestStore(t)
	seedDone(t, s, "ws1", 1)
	seedDone(t, s, "ws1", 2)
	if _, err := s.ArchiveDone("ws1"); err != nil {
		t.Fatalf("ArchiveDone: %v", err)
	}
	if err := s.UnarchiveIssue("ws1", 1); err != nil {
		t.Fatalf("UnarchiveIssue: %v", err)
	}
	live, _ := s.ListIssues("ws1")
	if len(live) != 1 || live[0].Number != 1 {
		t.Errorf("after unarchive ListIssues = %+v, want only #1", live)
	}
	arch, _ := s.ListArchivedIssues("ws1")
	if len(arch) != 1 || arch[0].Number != 2 {
		t.Errorf("archived = %+v, want only #2", arch)
	}
}

func TestUnarchiveAllRestoresEverything(t *testing.T) {
	s := newTestStore(t)
	seedDone(t, s, "ws1", 1)
	seedDone(t, s, "ws2", 2)
	if _, err := s.ArchiveDone(""); err != nil {
		t.Fatalf("ArchiveDone: %v", err)
	}
	if err := s.UnarchiveAll(""); err != nil {
		t.Fatalf("UnarchiveAll: %v", err)
	}
	arch, _ := s.ListArchivedIssues("")
	if len(arch) != 0 {
		t.Errorf("after UnarchiveAll archived = %d, want 0", len(arch))
	}
}

func TestArchiveDoneIdempotent(t *testing.T) {
	s := newTestStore(t)
	seedDone(t, s, "ws1", 1)
	if _, err := s.ArchiveDone("ws1"); err != nil {
		t.Fatalf("ArchiveDone first: %v", err)
	}
	n, err := s.ArchiveDone("ws1")
	if err != nil {
		t.Fatalf("ArchiveDone second: %v", err)
	}
	if n != 0 {
		t.Errorf("second ArchiveDone returned %d, want 0", n)
	}
}

// TestSaveIssuePreservesArchivedAtOnReopen pins the bug-fix behaviour: a
// state=open SaveIssue (e.g. the 5-minute GitHub poller's natural re-save of
// every still-open issue) MUST NOT clobber a user-set archived_at. The prior
// "auto-unarchive on reopen" arm fired on every poll and clobbered any card
// the user had archived from DONE — DONE is a board column, not a GitHub
// state, so most archived cards still have State=="open" upstream.
// Unarchive is now exclusively an explicit user action.
func TestSaveIssuePreservesArchivedAtOnReopen(t *testing.T) {
	s := newTestStore(t)
	if _, err := s.SaveIssue(types.Issue{
		WorkspaceID: "ws1",
		Number:      42,
		Title:       "thing",
		State:       "closed",
		Column:      types.ColDone,
	}); err != nil {
		t.Fatalf("seed: %v", err)
	}
	if _, err := s.ArchiveDone("ws1"); err != nil {
		t.Fatalf("ArchiveDone: %v", err)
	}
	// Now the GitHub poller re-fetches the issue and saves it again. The
	// archive must survive.
	unarchived, err := s.SaveIssue(types.Issue{
		WorkspaceID: "ws1",
		Number:      42,
		Title:       "thing (reopened)",
		State:       "open",
		UpdatedAt:   time.Now(),
	})
	if err != nil {
		t.Fatalf("SaveIssue reopen: %v", err)
	}
	if unarchived {
		t.Errorf("unarchived = true, want false (archive must be sticky)")
	}
	live, _ := s.ListIssues("ws1")
	if len(live) != 0 {
		t.Errorf("archived row should be excluded from ListIssues, got: %+v", live)
	}
	archived, _ := s.ListArchivedIssues("ws1")
	if len(archived) != 1 || archived[0].Number != 42 {
		t.Errorf("archived row should remain in ListArchivedIssues, got: %+v", archived)
	}
}

func TestSaveIssuePreservesArchivedAtOnClosedRoundtrip(t *testing.T) {
	s := newTestStore(t)
	if _, err := s.SaveIssue(types.Issue{
		WorkspaceID: "ws1",
		Number:      42,
		Title:       "thing",
		State:       "closed",
		Column:      types.ColDone,
	}); err != nil {
		t.Fatalf("seed: %v", err)
	}
	if _, err := s.ArchiveDone("ws1"); err != nil {
		t.Fatalf("ArchiveDone: %v", err)
	}
	// A subsequent state=closed save (e.g. a hypothetical poller pass that
	// somehow re-sees the row as closed) must not clear archived_at.
	unarchived, err := s.SaveIssue(types.Issue{
		WorkspaceID: "ws1",
		Number:      42,
		Title:       "thing again",
		State:       "closed",
		Column:      types.ColDone,
	})
	if err != nil {
		t.Fatalf("SaveIssue: %v", err)
	}
	if unarchived {
		t.Errorf("unarchived = true on state=closed save, want false")
	}
	arch, _ := s.ListArchivedIssues("ws1")
	if len(arch) != 1 {
		t.Errorf("archived row count = %d, want 1 (preserved)", len(arch))
	}
}

func TestAccumulateIssueWork(t *testing.T) {
	s := newTestStore(t)
	if _, err := s.SaveIssue(types.Issue{
		WorkspaceID: "ws1",
		Number:      10,
		Title:       "timer test",
		State:       "open",
	}); err != nil {
		t.Fatalf("SaveIssue: %v", err)
	}

	// First session: 60s of plan work.
	if err := s.AccumulateIssueWork("ws1", 10, types.ModePlan, 60); err != nil {
		t.Fatalf("AccumulateIssueWork plan: %v", err)
	}
	// Second session: 120s of execute work.
	if err := s.AccumulateIssueWork("ws1", 10, types.ModeExecute, 120); err != nil {
		t.Fatalf("AccumulateIssueWork execute: %v", err)
	}

	issues, err := s.ListIssues("ws1")
	if err != nil || len(issues) != 1 {
		t.Fatalf("ListIssues: %v len=%d", err, len(issues))
	}
	iss := issues[0]
	if iss.WorkSeconds != 180 {
		t.Errorf("WorkSeconds = %d, want 180", iss.WorkSeconds)
	}
	if iss.WorkSecondsPlan != 60 {
		t.Errorf("WorkSecondsPlan = %d, want 60", iss.WorkSecondsPlan)
	}
	if iss.WorkSecondsExecute != 120 {
		t.Errorf("WorkSecondsExecute = %d, want 120", iss.WorkSecondsExecute)
	}

	// Verify SaveIssue (GitHub poll) preserves accumulated work seconds.
	if _, err := s.SaveIssue(types.Issue{
		WorkspaceID: "ws1",
		Number:      10,
		Title:       "timer test updated",
		State:       "open",
	}); err != nil {
		t.Fatalf("SaveIssue (re-save): %v", err)
	}
	issues2, _ := s.ListIssues("ws1")
	if issues2[0].WorkSeconds != 180 {
		t.Errorf("WorkSeconds after re-save = %d, want 180 (should be preserved)", issues2[0].WorkSeconds)
	}

	// Verify zero-second calls are no-ops (no error, no change).
	if err := s.AccumulateIssueWork("ws1", 10, types.ModePlan, 0); err != nil {
		t.Errorf("AccumulateIssueWork with 0 seconds: %v", err)
	}
	issues3, _ := s.ListIssues("ws1")
	if issues3[0].WorkSeconds != 180 {
		t.Errorf("WorkSeconds after zero-second call = %d, want 180", issues3[0].WorkSeconds)
	}
}

func TestAccumulateIssueWorkMissingIssue(t *testing.T) {
	s := newTestStore(t)
	err := s.AccumulateIssueWork("ws1", 999, types.ModePlan, 30)
	if err == nil {
		t.Error("expected error for missing issue, got nil")
	}
}

func TestAccumulateIssueWorkPreservesExistingTime(t *testing.T) {
	s := newTestStore(t)
	if _, err := s.SaveIssue(types.Issue{
		WorkspaceID:        "ws1",
		Number:             20,
		Title:              "existing",
		State:              "open",
		WorkSeconds:        300,
		WorkSecondsPlan:    100,
		WorkSecondsExecute: 200,
	}); err != nil {
		t.Fatalf("SaveIssue: %v", err)
	}
	if err := s.AccumulateIssueWork("ws1", 20, types.ModeExecute, 50); err != nil {
		t.Fatalf("AccumulateIssueWork: %v", err)
	}
	issues, _ := s.ListIssues("ws1")
	iss := issues[0]
	if iss.WorkSeconds != 350 {
		t.Errorf("WorkSeconds = %d, want 350", iss.WorkSeconds)
	}
	if iss.WorkSecondsExecute != 250 {
		t.Errorf("WorkSecondsExecute = %d, want 250", iss.WorkSecondsExecute)
	}
	if iss.WorkSecondsPlan != 100 {
		t.Errorf("WorkSecondsPlan = %d, want 100 (unchanged)", iss.WorkSecondsPlan)
	}
}

func TestAccumulateIssueWorkTimestampPreserved(t *testing.T) {
	s := newTestStore(t)
	ts := time.Now().Add(-time.Hour)
	if _, err := s.SaveIssue(types.Issue{
		WorkspaceID: "ws1",
		Number:      30,
		Title:       "ts test",
		State:       "open",
		UpdatedAt:   ts,
	}); err != nil {
		t.Fatalf("SaveIssue: %v", err)
	}
	if err := s.AccumulateIssueWork("ws1", 30, types.ModePlan, 10); err != nil {
		t.Fatalf("AccumulateIssueWork: %v", err)
	}
	issues, _ := s.ListIssues("ws1")
	// updated_at should be unchanged by AccumulateIssueWork
	if issues[0].Title != "ts test" {
		t.Errorf("title changed after AccumulateIssueWork, got %q", issues[0].Title)
	}
}

func TestMarkIssueClosedSkipsArchived(t *testing.T) {
	s := newTestStore(t)
	if _, err := s.SaveIssue(types.Issue{
		WorkspaceID: "ws1",
		Number:      1,
		State:       "open",
		Column:      types.ColDone,
	}); err != nil {
		t.Fatal(err)
	}
	// Archive the card first.
	if _, err := s.ArchiveDone("ws1"); err != nil {
		t.Fatal(err)
	}
	// MarkIssueClosed must be a no-op for archived cards.
	if err := s.MarkIssueClosed("ws1", 1); err != nil {
		t.Fatalf("MarkIssueClosed: %v", err)
	}
	// The card should still be archived.
	archived, err := s.ListArchivedIssues("ws1")
	if err != nil {
		t.Fatal(err)
	}
	if len(archived) != 1 {
		t.Errorf("expected archived card to remain, got %d archived rows", len(archived))
	}
}

// ReconcileStaleRunningSessions finds session rows whose state is still
// running (or waiting_for_input) but whose owning issue is already in
// REVIEW or DONE. Those rows can only exist when a harness goroutine died
// without updating its DB state — the IssueView assembler then surfaces
// them as `active_session` and the card glows blue/purple in REVIEW/DONE
// forever. The reconciler marks them failed at startup so the UI clears.
func TestReconcileStaleRunningSessions(t *testing.T) {
	s := newTestStore(t)
	// Issue #1 is DONE with a PR merged. A stale running session sits on it.
	if _, err := s.SaveIssue(types.Issue{
		WorkspaceID: "ws1",
		Number:      1,
		State:       "closed",
		Column:      types.ColDone,
	}); err != nil {
		t.Fatal(err)
	}
	stale := &types.Session{
		ID:          "sess-stale-done",
		WorkspaceID: "ws1",
		IssueNumber: 1,
		Mode:        types.ModePlan,
		State:       types.StateRunning,
		StartedAt:   time.Now(),
	}
	if err := s.SaveSession(stale, ""); err != nil {
		t.Fatalf("SaveSession stale: %v", err)
	}
	// Issue #2 is in REVIEW with a stale running execute session.
	if _, err := s.SaveIssue(types.Issue{
		WorkspaceID: "ws1",
		Number:      2,
		State:       "open",
		Column:      types.ColReview,
	}); err != nil {
		t.Fatal(err)
	}
	staleRev := &types.Session{
		ID:          "sess-stale-review",
		WorkspaceID: "ws1",
		IssueNumber: 2,
		Mode:        types.ModeExecute,
		State:       types.StateRunning,
		StartedAt:   time.Now(),
	}
	if err := s.SaveSession(staleRev, ""); err != nil {
		t.Fatalf("SaveSession stale review: %v", err)
	}
	// Issue #3 is in PLAN with a legitimately running plan session — must NOT be touched.
	if _, err := s.SaveIssue(types.Issue{
		WorkspaceID: "ws1",
		Number:      3,
		State:       "open",
		Column:      types.ColPlan,
	}); err != nil {
		t.Fatal(err)
	}
	live := &types.Session{
		ID:          "sess-live",
		WorkspaceID: "ws1",
		IssueNumber: 3,
		Mode:        types.ModePlan,
		State:       types.StateRunning,
		StartedAt:   time.Now(),
	}
	if err := s.SaveSession(live, ""); err != nil {
		t.Fatalf("SaveSession live: %v", err)
	}

	n, err := s.ReconcileStaleRunningSessions()
	if err != nil {
		t.Fatalf("ReconcileStaleRunningSessions: %v", err)
	}
	if n != 2 {
		t.Errorf("reconciled %d sessions, want 2 (the DONE + REVIEW stale rows)", n)
	}

	// Verify both indexed state column AND $.state JSON blob got updated for stale rows.
	for _, id := range []string{"sess-stale-done", "sess-stale-review"} {
		var col, jstate string
		if err := s.DB.QueryRow(
			`SELECT state, json_extract(json,'$.state') FROM sessions WHERE id = ?`, id,
		).Scan(&col, &jstate); err != nil {
			t.Fatalf("read %s: %v", id, err)
		}
		if col != "failed" {
			t.Errorf("%s: indexed state=%q, want failed", id, col)
		}
		if jstate != "failed" {
			t.Errorf("%s: json.state=%q, want failed", id, jstate)
		}
	}

	// The live PLAN session must still be running.
	var liveState string
	if err := s.DB.QueryRow(`SELECT state FROM sessions WHERE id = 'sess-live'`).Scan(&liveState); err != nil {
		t.Fatalf("read live: %v", err)
	}
	if liveState != "running" {
		t.Errorf("live PLAN session was wrongly reconciled, state=%q", liveState)
	}
}

func TestReconcileClosedIssuesSkipsArchived(t *testing.T) {
	s := newTestStore(t)
	// Closed issue stuck in TODO — should be reconciled.
	if _, err := s.SaveIssue(types.Issue{
		WorkspaceID: "ws1",
		Number:      2,
		State:       "closed",
		Column:      types.ColTodo,
	}); err != nil {
		t.Fatal(err)
	}
	// Archived closed card — must be ignored by reconcile.
	if _, err := s.SaveIssue(types.Issue{
		WorkspaceID: "ws1",
		Number:      3,
		State:       "closed",
		Column:      types.ColDone,
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.ArchiveDone("ws1"); err != nil {
		t.Fatal(err)
	}
	n, err := s.ReconcileClosedIssues()
	if err != nil {
		t.Fatal(err)
	}
	// Only issue #2 should be reconciled; #3 is archived and must be skipped.
	if n != 1 {
		t.Errorf("expected 1 reconciled, got %d", n)
	}
	archived, _ := s.ListArchivedIssues("ws1")
	if len(archived) != 1 || archived[0].Number != 3 {
		t.Errorf("archived card should still be #3, got %v", archived)
	}
}

func TestArchiveClosedByAgeBasic(t *testing.T) {
	s := newTestStore(t)
	if _, err := s.SaveIssue(types.Issue{
		WorkspaceID: "ws1",
		Number:      10,
		State:       "closed",
		Column:      types.ColDone,
	}); err != nil {
		t.Fatal(err)
	}
	// Manually set closed_at to 8 days ago.
	past := time.Now().UTC().Add(-8 * 24 * time.Hour).Unix()
	if _, err := s.DB.Exec(`UPDATE issues SET closed_at = ? WHERE workspace_id = 'ws1' AND number = 10`, past); err != nil {
		t.Fatal(err)
	}
	n, err := s.ArchiveClosedByAge("ws1", 7)
	if err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Errorf("expected 1 archived, got %d", n)
	}
}

func TestArchiveClosedByAgeSkipsRecent(t *testing.T) {
	s := newTestStore(t)
	if _, err := s.SaveIssue(types.Issue{
		WorkspaceID: "ws1",
		Number:      11,
		State:       "closed",
		Column:      types.ColDone,
	}); err != nil {
		t.Fatal(err)
	}
	// closed_at only 2 days ago — should NOT be archived with a 7-day threshold.
	recent := time.Now().UTC().Add(-2 * 24 * time.Hour).Unix()
	if _, err := s.DB.Exec(`UPDATE issues SET closed_at = ? WHERE workspace_id = 'ws1' AND number = 11`, recent); err != nil {
		t.Fatal(err)
	}
	n, err := s.ArchiveClosedByAge("ws1", 7)
	if err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Errorf("expected 0 archived (too recent), got %d", n)
	}
}

func TestArchiveClosedByAgeSkipsNullClosedAt(t *testing.T) {
	s := newTestStore(t)
	if _, err := s.SaveIssue(types.Issue{
		WorkspaceID: "ws1",
		Number:      12,
		State:       "closed",
		Column:      types.ColDone,
	}); err != nil {
		t.Fatal(err)
	}
	// closed_at is NULL — must not be archived.
	n, err := s.ArchiveClosedByAge("ws1", 7)
	if err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Errorf("expected 0 archived (null closed_at), got %d", n)
	}
}

func TestArchiveClosedByAgeSkipsNonDone(t *testing.T) {
	s := newTestStore(t)
	if _, err := s.SaveIssue(types.Issue{
		WorkspaceID: "ws1",
		Number:      13,
		State:       "closed",
		Column:      types.ColInProgress,
	}); err != nil {
		t.Fatal(err)
	}
	past := time.Now().UTC().Add(-10 * 24 * time.Hour).Unix()
	if _, err := s.DB.Exec(`UPDATE issues SET closed_at = ? WHERE workspace_id = 'ws1' AND number = 13`, past); err != nil {
		t.Fatal(err)
	}
	n, err := s.ArchiveClosedByAge("ws1", 7)
	if err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Errorf("expected 0 archived (non-done column), got %d", n)
	}
}

func TestMarkIssueClosedSetsClosedAt(t *testing.T) {
	s := newTestStore(t)
	if _, err := s.SaveIssue(types.Issue{
		WorkspaceID: "ws1",
		Number:      20,
		State:       "open",
		Column:      types.ColTodo,
	}); err != nil {
		t.Fatal(err)
	}
	before := time.Now().UTC().Add(-time.Second)
	if err := s.MarkIssueClosed("ws1", 20); err != nil {
		t.Fatal(err)
	}
	var closedAt *int64
	var raw int64
	err := s.DB.QueryRow(`SELECT closed_at FROM issues WHERE workspace_id = 'ws1' AND number = 20`).Scan(&raw)
	if err == nil {
		closedAt = &raw
	}
	if closedAt == nil {
		t.Fatal("expected closed_at to be set, got NULL")
	}
	if *closedAt < before.Unix() {
		t.Errorf("closed_at %d is before test start %d", *closedAt, before.Unix())
	}
}
