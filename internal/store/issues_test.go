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

func TestSaveIssueAutoUnarchivesOnReopen(t *testing.T) {
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
	// Now GitHub poll shows it state=open again.
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
	if !unarchived {
		t.Errorf("unarchived = false, want true")
	}
	live, _ := s.ListIssues("ws1")
	if len(live) != 1 || live[0].Number != 42 {
		t.Errorf("reopened row missing from ListIssues: %+v", live)
	}
	if live[0].ArchivedAt != nil {
		t.Errorf("ArchivedAt = %v, want nil after reopen", live[0].ArchivedAt)
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
