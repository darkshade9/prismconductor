package store

import (
	"database/sql"
	"time"

	"prismconductor/internal/types"
)

// UpsertPRComment inserts a PR comment row. Returns true if the row was newly
// inserted (i.e. not seen before), false if it already existed (UNIQUE
// constraint on workspace_id+issue_number+comment_id). Issue #159.
func (s *Store) UpsertPRComment(c types.PRComment) (bool, error) {
	res, err := s.DB.Exec(
		`INSERT OR IGNORE INTO pr_comments
		    (workspace_id, issue_number, comment_id, author, body, kind, file_path, line_number, created_at, pending_post)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		c.WorkspaceID, c.IssueNumber, c.CommentID,
		c.Author, c.Body, string(c.Kind),
		nullText(c.FilePath), nullInt(c.LineNumber),
		c.CreatedAt.Unix(),
		boolToInt(c.PendingPost),
	)
	if err != nil {
		return false, err
	}
	n, _ := res.RowsAffected()
	return n > 0, nil
}

// MarkPRCommentRead sets read_at = now for a single comment. No-op if already read.
func (s *Store) MarkPRCommentRead(workspaceID string, issueNumber int, commentID int64) error {
	_, err := s.DB.Exec(
		`UPDATE pr_comments SET read_at = ? WHERE workspace_id = ? AND issue_number = ? AND comment_id = ? AND read_at IS NULL`,
		time.Now().Unix(), workspaceID, issueNumber, commentID,
	)
	return err
}

// MarkAllPRCommentsRead sets read_at = now for every unread comment on an issue.
func (s *Store) MarkAllPRCommentsRead(workspaceID string, issueNumber int) error {
	_, err := s.DB.Exec(
		`UPDATE pr_comments SET read_at = ? WHERE workspace_id = ? AND issue_number = ? AND read_at IS NULL`,
		time.Now().Unix(), workspaceID, issueNumber,
	)
	return err
}

// ListUnreadPRComments returns all unread (read_at IS NULL) comments for an issue,
// ordered oldest-first so the modal shows the conversation in chronological order.
func (s *Store) ListUnreadPRComments(workspaceID string, issueNumber int) ([]types.PRComment, error) {
	rows, err := s.DB.Query(
		`SELECT comment_id, author, body, kind, COALESCE(file_path,''), COALESCE(line_number,0), created_at, pending_post
		   FROM pr_comments
		  WHERE workspace_id = ? AND issue_number = ? AND read_at IS NULL
		  ORDER BY created_at ASC`,
		workspaceID, issueNumber,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanPRComments(workspaceID, issueNumber, rows)
}

// ListPRComments returns all comments for an issue (read and unread), ordered
// oldest-first. Used by the full PR conversation panel.
func (s *Store) ListPRComments(workspaceID string, issueNumber int) ([]types.PRComment, error) {
	rows, err := s.DB.Query(
		`SELECT comment_id, author, body, kind, COALESCE(file_path,''), COALESCE(line_number,0), created_at, pending_post
		   FROM pr_comments
		  WHERE workspace_id = ? AND issue_number = ?
		  ORDER BY created_at ASC`,
		workspaceID, issueNumber,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanPRComments(workspaceID, issueNumber, rows)
}

func scanPRComments(workspaceID string, issueNumber int, rows *sql.Rows) ([]types.PRComment, error) {
	var out []types.PRComment
	for rows.Next() {
		var c types.PRComment
		var createdUnix int64
		var kind string
		c.WorkspaceID = workspaceID
		c.IssueNumber = issueNumber
		if err := rows.Scan(&c.CommentID, &c.Author, &c.Body, &kind, &c.FilePath, &c.LineNumber, &createdUnix, &c.PendingPost); err != nil {
			return nil, err
		}
		c.Kind = types.PRCommentKind(kind)
		c.CreatedAt = time.Unix(createdUnix, 0).UTC()
		out = append(out, c)
	}
	return out, rows.Err()
}

// helpers

func nullText(s string) any {
	if s == "" {
		return nil
	}
	return s
}

func nullInt(n int) any {
	if n == 0 {
		return nil
	}
	return n
}

func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}
