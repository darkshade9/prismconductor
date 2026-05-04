package store

import (
	"testing"
	"time"

	"prismconductor/internal/types"
)

func TestPRComments(t *testing.T) {
	dir := t.TempDir()
	s, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	c := types.PRComment{
		WorkspaceID: "ws1",
		IssueNumber: 42,
		CommentID:   1001,
		Author:      "alice",
		Body:        "LGTM",
		Kind:        types.PRCommentKindConversation,
		CreatedAt:   time.Now().UTC().Truncate(time.Second),
	}

	// First insert should be new.
	isNew, err := s.UpsertPRComment(c)
	if err != nil {
		t.Fatalf("UpsertPRComment: %v", err)
	}
	if !isNew {
		t.Error("expected isNew=true on first insert")
	}

	// Duplicate insert should be idempotent.
	isNew2, err := s.UpsertPRComment(c)
	if err != nil {
		t.Fatalf("UpsertPRComment duplicate: %v", err)
	}
	if isNew2 {
		t.Error("expected isNew=false on duplicate insert")
	}

	// Should appear in unread list.
	unread, err := s.ListUnreadPRComments("ws1", 42)
	if err != nil {
		t.Fatalf("ListUnreadPRComments: %v", err)
	}
	if len(unread) != 1 {
		t.Fatalf("expected 1 unread, got %d", len(unread))
	}
	if unread[0].Author != "alice" {
		t.Errorf("unexpected author %q", unread[0].Author)
	}

	// Mark read.
	if err := s.MarkPRCommentRead("ws1", 42, 1001); err != nil {
		t.Fatalf("MarkPRCommentRead: %v", err)
	}

	// Should no longer be unread.
	unread2, _ := s.ListUnreadPRComments("ws1", 42)
	if len(unread2) != 0 {
		t.Errorf("expected 0 unread after mark-read, got %d", len(unread2))
	}

	// Still in full list.
	all, err := s.ListPRComments("ws1", 42)
	if err != nil {
		t.Fatalf("ListPRComments: %v", err)
	}
	if len(all) != 1 {
		t.Fatalf("expected 1 comment in full list, got %d", len(all))
	}
}

func TestPRCommentsMarkAllRead(t *testing.T) {
	dir := t.TempDir()
	s, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	for i := int64(1); i <= 3; i++ {
		_, _ = s.UpsertPRComment(types.PRComment{
			WorkspaceID: "ws1",
			IssueNumber: 7,
			CommentID:   i,
			Author:      "bob",
			Body:        "comment",
			Kind:        types.PRCommentKindReview,
			CreatedAt:   time.Now().UTC(),
		})
	}

	unread, _ := s.ListUnreadPRComments("ws1", 7)
	if len(unread) != 3 {
		t.Fatalf("expected 3 unread, got %d", len(unread))
	}

	if err := s.MarkAllPRCommentsRead("ws1", 7); err != nil {
		t.Fatalf("MarkAllPRCommentsRead: %v", err)
	}

	unread2, _ := s.ListUnreadPRComments("ws1", 7)
	if len(unread2) != 0 {
		t.Errorf("expected 0 unread after MarkAll, got %d", len(unread2))
	}
}

// Ensure the migration adds the expected table (integration smoke).
func TestPRCommentsTableExists(t *testing.T) {
	dir := t.TempDir()
	s, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	var name string
	if err := s.DB.QueryRow(`SELECT name FROM sqlite_master WHERE type='table' AND name='pr_comments'`).Scan(&name); err != nil {
		t.Fatalf("pr_comments table not found: %v", err)
	}
	if name != "pr_comments" {
		t.Errorf("unexpected table name %q", name)
	}
}
