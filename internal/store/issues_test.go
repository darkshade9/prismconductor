package store

import (
	"testing"

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
	if err := s.SaveIssue(types.Issue{
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
	if err := s.SaveIssue(types.Issue{
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
	if err := s.SaveIssue(types.Issue{
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
	if err := s.SaveIssue(types.Issue{
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
	if err := s.SaveIssue(types.Issue{
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
	if err := s.SaveIssue(types.Issue{
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
	if err := s.SaveIssue(types.Issue{
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
