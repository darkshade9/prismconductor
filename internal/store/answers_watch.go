package store

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"prismconductor/internal/types"
)

// PausedSession is the watcher's view of a session that's currently waiting on
// a mid-run question. Returned from the PausedFn callback.
type PausedSession struct {
	SessionID   string
	WorkspaceID string
	IssueNumber int
	QuestionID  string
}

// AnswerWatcher polls each enabled workspace's `.prismconductor/answers/` dir
// on a 1s ticker (issue #17, Q1=A) and fires OnAnswer when an answer file
// matches a session that's paused on that question.
//
// The watcher does NOT distinguish "answer arrived live" from "answer was
// already on disk at startup" (Q4=A) — first tick after startup will auto-
// resume any pre-existing match.
//
// Idempotency: each (sessionID, questionID) pair fires at most once for the
// watcher's lifetime; the resume path is expected to clear pending_question_id
// on the old session row so subsequent paused-session lookups won't surface
// the stale match.
type AnswerWatcher struct {
	interval     time.Duration
	workspacesFn func() []types.Workspace
	pausedFn     func() []PausedSession
	onAnswer     func(PausedSession)

	mu    sync.Mutex
	fired map[string]bool
}

func NewAnswerWatcher(
	interval time.Duration,
	workspacesFn func() []types.Workspace,
	pausedFn func() []PausedSession,
	onAnswer func(PausedSession),
) *AnswerWatcher {
	if interval <= 0 {
		interval = time.Second
	}
	return &AnswerWatcher{
		interval:     interval,
		workspacesFn: workspacesFn,
		pausedFn:     pausedFn,
		onAnswer:     onAnswer,
		fired:        map[string]bool{},
	}
}

// Run blocks until ctx is done, ticking every interval. Spawn in a goroutine.
func (w *AnswerWatcher) Run(ctx context.Context) {
	if w == nil {
		return
	}
	t := time.NewTicker(w.interval)
	defer t.Stop()
	w.Tick()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			w.Tick()
		}
	}
}

// Tick runs one watcher pass. Exposed for tests.
func (w *AnswerWatcher) Tick() {
	if w == nil || w.workspacesFn == nil || w.pausedFn == nil || w.onAnswer == nil {
		return
	}
	paused := w.pausedFn()
	if len(paused) == 0 {
		return
	}
	// Index paused sessions by (workspaceID, questionID) so each answer file
	// lookup is O(1).
	idx := make(map[string]PausedSession, len(paused))
	for _, p := range paused {
		idx[p.WorkspaceID+"|"+p.QuestionID] = p
	}
	for _, ws := range w.workspacesFn() {
		if !ws.Enabled || ws.RepoPath == "" {
			continue
		}
		dir := filepath.Join(ws.RepoPath, ".prismconductor", "answers")
		entries, err := os.ReadDir(dir)
		if err != nil {
			continue
		}
		for _, e := range entries {
			if e.IsDir() {
				continue
			}
			name := e.Name()
			if !strings.HasSuffix(name, ".json") {
				continue
			}
			// Mid-run answer files are named "<question-uuid>.json" — distinct
			// from the rev-keyed plan-mode answers ("<num>-rev<N>.json"). The
			// rev-keyed ones contain a "-" before "rev"; question UUIDs are
			// "8-4-4-4-12" hex with no "rev" segment. Skip anything that
			// matches the rev pattern so SubmitAnswers files don't trigger.
			id := strings.TrimSuffix(name, ".json")
			if strings.Contains(id, "-rev") {
				continue
			}
			ps, ok := idx[ws.ID+"|"+id]
			if !ok {
				continue
			}
			key := ps.SessionID + ":" + ps.QuestionID
			w.mu.Lock()
			if w.fired[key] {
				w.mu.Unlock()
				continue
			}
			w.fired[key] = true
			w.mu.Unlock()
			w.onAnswer(ps)
		}
	}
}
