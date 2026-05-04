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
//
// Orphan detection (#153): if SetOrphanCallback is called with a non-nil fn,
// the watcher also tracks sessions whose question file is absent from disk.
// When a session's question file has been missing for longer than gracePeriod,
// the onOrphanQuestion callback is invoked (at most once per session).
type AnswerWatcher struct {
	interval     time.Duration
	workspacesFn func() []types.Workspace
	pausedFn     func() []PausedSession
	onAnswer     func(PausedSession)

	mu    sync.Mutex
	fired map[string]bool

	// Orphan detection fields (#153). All guarded by mu.
	onOrphanQuestion func(PausedSession)
	gracePeriod      time.Duration
	firstMissing     map[string]time.Time // sessionID → time question file first absent
	orphanFired      map[string]bool       // sessionID → already fired onOrphanQuestion
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
		firstMissing: map[string]time.Time{},
		orphanFired:  map[string]bool{},
	}
}

// SetOrphanCallback enables orphan detection (#153). After gracePeriod of
// continuous question-file absence for a paused session, fn is called once.
// Must be called before Run/Tick. A nil fn disables orphan detection.
func (w *AnswerWatcher) SetOrphanCallback(gracePeriod time.Duration, fn func(PausedSession)) {
	if w == nil {
		return
	}
	w.mu.Lock()
	w.gracePeriod = gracePeriod
	w.onOrphanQuestion = fn
	w.mu.Unlock()
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
	// Build a workspace-path index for O(1) access during orphan probing.
	wsPath := make(map[string]string)
	for _, ws := range w.workspacesFn() {
		wsPath[ws.ID] = ws.RepoPath
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

	// Orphan detection: for each paused session, probe the question file.
	// If absent for > gracePeriod, fire onOrphanQuestion once (#153).
	w.mu.Lock()
	orphanFn := w.onOrphanQuestion
	grace := w.gracePeriod
	w.mu.Unlock()
	if orphanFn == nil || grace <= 0 {
		return
	}
	now := time.Now()
	for _, ps := range paused {
		if ps.QuestionID == "" {
			continue
		}
		repoPath := wsPath[ps.WorkspaceID]
		if repoPath == "" {
			continue
		}
		qPath := filepath.Join(repoPath, ".prismconductor", "questions", ps.QuestionID+".json")
		_, statErr := os.Stat(qPath)
		questionExists := statErr == nil

		w.mu.Lock()
		if questionExists {
			// Question file back on disk (e.g. worker re-wrote it) — reset timer.
			delete(w.firstMissing, ps.SessionID)
			w.mu.Unlock()
			continue
		}
		if w.orphanFired[ps.SessionID] {
			w.mu.Unlock()
			continue
		}
		first, seen := w.firstMissing[ps.SessionID]
		if !seen {
			w.firstMissing[ps.SessionID] = now
			w.mu.Unlock()
			continue
		}
		if now.Sub(first) < grace {
			w.mu.Unlock()
			continue
		}
		w.orphanFired[ps.SessionID] = true
		w.mu.Unlock()
		orphanFn(ps)
	}
}
