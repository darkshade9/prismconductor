package store

import (
	"os"
	"path/filepath"
	"testing"

	"prismconductor/internal/types"
)

func writeAnswerFile(t *testing.T, repoPath, questionID, body string) {
	t.Helper()
	dir := filepath.Join(repoPath, ".prismconductor", "answers")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir answers: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, questionID+".json"), []byte(body), 0o644); err != nil {
		t.Fatalf("write answer: %v", err)
	}
}

func newWS(t *testing.T, id string) types.Workspace {
	t.Helper()
	return types.Workspace{
		ID:       id,
		RepoPath: t.TempDir(),
		Enabled:  true,
	}
}

func TestAnswerWatcherFiresForMatchingPausedSession(t *testing.T) {
	ws := newWS(t, "ws1")
	const qid = "11111111-2222-3333-4444-555555555555"
	writeAnswerFile(t, ws.RepoPath, qid, `{"question_id":"`+qid+`","answer":"yes"}`)

	paused := []PausedSession{{
		SessionID:   "sess-1",
		WorkspaceID: ws.ID,
		IssueNumber: 42,
		QuestionID:  qid,
	}}

	var fired []PausedSession
	w := NewAnswerWatcher(0,
		func() []types.Workspace { return []types.Workspace{ws} },
		func() []PausedSession { return paused },
		func(ps PausedSession) { fired = append(fired, ps) },
	)
	w.Tick()
	if len(fired) != 1 {
		t.Fatalf("expected 1 callback, got %d", len(fired))
	}
	if fired[0].SessionID != "sess-1" || fired[0].IssueNumber != 42 || fired[0].QuestionID != qid {
		t.Errorf("callback payload mismatch: %+v", fired[0])
	}
}

func TestAnswerWatcherIgnoresUnmatchedAnswers(t *testing.T) {
	ws := newWS(t, "ws1")
	const qid = "11111111-2222-3333-4444-555555555555"
	writeAnswerFile(t, ws.RepoPath, qid, `{}`)

	// No matching paused session.
	paused := []PausedSession{}
	var fired []PausedSession
	w := NewAnswerWatcher(0,
		func() []types.Workspace { return []types.Workspace{ws} },
		func() []PausedSession { return paused },
		func(ps PausedSession) { fired = append(fired, ps) },
	)
	w.Tick()
	if len(fired) != 0 {
		t.Errorf("expected no callbacks, got %d", len(fired))
	}
}

func TestAnswerWatcherIdempotent(t *testing.T) {
	ws := newWS(t, "ws1")
	const qid = "11111111-2222-3333-4444-555555555555"
	writeAnswerFile(t, ws.RepoPath, qid, `{}`)

	paused := []PausedSession{{
		SessionID:   "sess-1",
		WorkspaceID: ws.ID,
		IssueNumber: 42,
		QuestionID:  qid,
	}}

	var fired []PausedSession
	w := NewAnswerWatcher(0,
		func() []types.Workspace { return []types.Workspace{ws} },
		func() []PausedSession { return paused },
		func(ps PausedSession) { fired = append(fired, ps) },
	)
	w.Tick()
	w.Tick()
	w.Tick()
	if len(fired) != 1 {
		t.Errorf("expected exactly one callback across multiple ticks, got %d", len(fired))
	}
}

func TestAnswerWatcherSkipsRevKeyedAnswers(t *testing.T) {
	ws := newWS(t, "ws1")
	// Plan-mode answer file (rev-keyed) — must be skipped.
	writeAnswerFile(t, ws.RepoPath, "42-rev2", `{}`)

	paused := []PausedSession{{
		SessionID:   "sess-1",
		WorkspaceID: ws.ID,
		IssueNumber: 42,
		QuestionID:  "42-rev2",
	}}

	var fired []PausedSession
	w := NewAnswerWatcher(0,
		func() []types.Workspace { return []types.Workspace{ws} },
		func() []PausedSession { return paused },
		func(ps PausedSession) { fired = append(fired, ps) },
	)
	w.Tick()
	if len(fired) != 0 {
		t.Errorf("expected no callbacks for rev-keyed answer file, got %d", len(fired))
	}
}

func TestAnswerWatcherSkipsDisabledWorkspace(t *testing.T) {
	ws := newWS(t, "ws1")
	ws.Enabled = false
	const qid = "11111111-2222-3333-4444-555555555555"
	writeAnswerFile(t, ws.RepoPath, qid, `{}`)

	paused := []PausedSession{{
		SessionID:   "sess-1",
		WorkspaceID: ws.ID,
		IssueNumber: 42,
		QuestionID:  qid,
	}}

	var fired []PausedSession
	w := NewAnswerWatcher(0,
		func() []types.Workspace { return []types.Workspace{ws} },
		func() []PausedSession { return paused },
		func(ps PausedSession) { fired = append(fired, ps) },
	)
	w.Tick()
	if len(fired) != 0 {
		t.Errorf("expected no callbacks for disabled workspace, got %d", len(fired))
	}
}
