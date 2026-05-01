package session

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"prismconductor/internal/types"
)

func TestMapTerminalStatePausedSurvivesCleanExit(t *testing.T) {
	got := mapTerminalState(types.StatePausedForQuestion, nil)
	if got != types.StatePausedForQuestion {
		t.Errorf("paused + clean exit → %s, want paused_for_question", got)
	}
}

func TestMapTerminalStatePausedSurvivesNonZeroExit(t *testing.T) {
	got := mapTerminalState(types.StatePausedForQuestion, errors.New("exit 1"))
	if got != types.StatePausedForQuestion {
		t.Errorf("paused + non-zero exit → %s, want paused_for_question", got)
	}
}

func TestMapTerminalStateRunningCleanExitCompletes(t *testing.T) {
	got := mapTerminalState(types.StateRunning, nil)
	if got != types.StateCompleted {
		t.Errorf("running + clean exit → %s, want completed", got)
	}
}

func TestMapTerminalStateBlockedCleanExitFails(t *testing.T) {
	got := mapTerminalState(types.StateBlocked, nil)
	if got != types.StateFailed {
		t.Errorf("blocked + clean exit → %s, want failed", got)
	}
}

func TestMapTerminalStateWaitingCleanExitFails(t *testing.T) {
	got := mapTerminalState(types.StateWaitingForInput, nil)
	if got != types.StateFailed {
		t.Errorf("waiting + clean exit → %s, want failed", got)
	}
}

func TestMapTerminalStateNonZeroExitFailsByDefault(t *testing.T) {
	got := mapTerminalState(types.StateRunning, errors.New("exit 137"))
	if got != types.StateFailed {
		t.Errorf("running + non-zero exit → %s, want failed", got)
	}
}

// fakePersister records UpdateSessionState calls without touching SQLite.
type fakePersister struct {
	saves     []types.Session
	stateUpd  []string
	pendUpd   map[string]string
	offsetUpd map[string]int64
}

func (p *fakePersister) SaveSession(s *types.Session, _ string) error {
	cp := *s
	p.saves = append(p.saves, cp)
	return nil
}
func (p *fakePersister) UpdateSessionState(id string, state types.SessionState) error {
	p.stateUpd = append(p.stateUpd, id+":"+string(state))
	return nil
}
func (p *fakePersister) UpdateSessionPendingQuestion(id, qid string) error {
	if p.pendUpd == nil {
		p.pendUpd = map[string]string{}
	}
	p.pendUpd[id] = qid
	return nil
}
func (p *fakePersister) UpdateSessionTranscriptOffset(id string, off int64) error {
	if p.offsetUpd == nil {
		p.offsetUpd = map[string]int64{}
	}
	p.offsetUpd[id] = off
	return nil
}
func (p *fakePersister) AccumulateIssueWork(_ string, _ int, _ types.SessionMode, _ int64) error {
	return nil
}

func TestMatchPatternsQuestionPending(t *testing.T) {
	const qid = "11111111-2222-3333-4444-555555555555"
	store := &fakePersister{}
	m := &Manager{store: store}
	rs := &runtimeSession{sess: &types.Session{ID: "sess-1", State: types.StateRunning}}
	m.matchPatterns(rs, "@asst QUESTION_PENDING: "+qid)
	if rs.sess.State != types.StatePausedForQuestion {
		t.Fatalf("state after QUESTION_PENDING = %s, want paused_for_question", rs.sess.State)
	}
	if rs.sess.PendingQuestionID != qid {
		t.Errorf("pending question id = %q, want %q", rs.sess.PendingQuestionID, qid)
	}
	if len(store.saves) == 0 {
		t.Errorf("expected SaveSession to be called once on pause-transition")
	}
}

func TestMatchPatternsQuestionPendingIgnoresSecondInFlight(t *testing.T) {
	store := &fakePersister{}
	m := &Manager{store: store}
	rs := &runtimeSession{sess: &types.Session{
		ID:                "sess-1",
		State:             types.StatePausedForQuestion,
		PendingQuestionID: "first-id",
	}}
	m.matchPatterns(rs, "QUESTION_PENDING: second-id")
	if rs.sess.PendingQuestionID != "first-id" {
		t.Errorf("expected first-id to stick, got %q", rs.sess.PendingQuestionID)
	}
}

func TestMatchPatternsQuestionPendingEmptyIDIgnored(t *testing.T) {
	store := &fakePersister{}
	m := &Manager{store: store}
	rs := &runtimeSession{sess: &types.Session{ID: "sess-1", State: types.StateRunning}}
	m.matchPatterns(rs, "QUESTION_PENDING: ")
	if rs.sess.State != types.StateRunning {
		t.Errorf("empty id should be ignored, state = %s", rs.sess.State)
	}
	if rs.sess.PendingQuestionID != "" {
		t.Errorf("empty id should not set PendingQuestionID, got %q", rs.sess.PendingQuestionID)
	}
}

// TestSpawnWritesToTranscriptFile is the issue #54 survival test. It spawns a
// short shell script and asserts that:
//
//  1. The worker's stdout lands in the conductor's transcript file (proving
//     the file-IS-stdout wiring works).
//  2. The conductor's tail goroutine reads back the same lines and runs them
//     through matchPatterns — i.e. a "Work complete." printed by the worker
//     transitions the persisted state to completed.
//
// We don't actually kill the conductor goroutine here (Go test runners have
// no clean way to do that without leaking). The detached-from-conductor side
// is exercised by the SysProcAttr.Setsid in spawn_attr_unix.go, which is
// covered by detachedSupported() returning true on POSIX.
func TestSpawnWritesToTranscriptFile(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("survival path is POSIX-only; Windows variant is out of scope (#54 Non-goals)")
	}
	tdir := t.TempDir()
	transcripts := filepath.Join(tdir, "transcripts")
	store := &fakePersister{}
	m := NewManager(nil, nil)
	m.Configure(transcripts, store, nil)

	ws := types.Workspace{ID: "ws-1", RepoPath: tdir}
	issue := types.Issue{Number: 1, WorkspaceID: ws.ID}
	sess, err := m.spawnWithDir(ws, issue, types.ModeExecute,
		[]string{"/bin/sh", "-c", "echo hello-from-worker; echo Work complete."},
		"", "", "", types.Pool{}, "")
	if err != nil {
		t.Fatalf("spawnWithDir: %v", err)
	}

	// Wait until tailAndParse has finished. The goroutine deletes the session
	// from m.sessions on exit; poll until it's gone or we time out.
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		m.mu.RLock()
		_, stillThere := m.sessions[sess.ID]
		m.mu.RUnlock()
		if !stillThere {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	m.mu.RLock()
	_, stillThere := m.sessions[sess.ID]
	m.mu.RUnlock()
	if stillThere {
		t.Fatalf("tailAndParse goroutine never exited within deadline")
	}

	// The transcript on disk must contain exactly what the worker wrote.
	b, err := os.ReadFile(sess.Transcript)
	if err != nil {
		t.Fatalf("read transcript: %v", err)
	}
	got := string(b)
	if !strings.Contains(got, "hello-from-worker") || !strings.Contains(got, "Work complete.") {
		t.Errorf("transcript missing worker output, got: %q", got)
	}

	// matchPatterns should have transitioned the session to completed.
	found := false
	for _, upd := range store.stateUpd {
		if strings.HasSuffix(upd, ":completed") {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected a state update ending in :completed, got %v", store.stateUpd)
	}

	// transcript_offset must have been persisted at least once during the
	// final flush.
	if _, ok := store.offsetUpd[sess.ID]; !ok {
		t.Errorf("expected transcript_offset to be persisted at least once")
	}
}

// TestSpawnPersistsPoolID verifies that PoolID from the spawning pool is
// recorded on the session row (issue #37).
func TestSpawnPersistsPoolID(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX-only")
	}
	tdir := t.TempDir()
	store := &fakePersister{}
	m := NewManager(nil, nil)
	m.Configure(filepath.Join(tdir, "transcripts"), store, nil)

	ws := types.Workspace{ID: "ws-1", RepoPath: tdir}
	issue := types.Issue{Number: 2, WorkspaceID: ws.ID}
	pool := types.Pool{ID: "pool-xyz", Name: "test", Provider: "claude"}

	sess, err := m.spawnWithDir(ws, issue, types.ModeExecute,
		[]string{"/bin/sh", "-c", "echo Work complete."},
		"", "", "", pool, "")
	if err != nil {
		t.Fatalf("spawnWithDir: %v", err)
	}
	if sess.PoolID != "pool-xyz" {
		t.Errorf("sess.PoolID = %q, want %q", sess.PoolID, "pool-xyz")
	}
	// The persisted JSON blob must also carry the pool_id.
	if len(store.saves) == 0 {
		t.Fatalf("expected SaveSession to be called")
	}
	saved := store.saves[0]
	if saved.PoolID != "pool-xyz" {
		t.Errorf("persisted PoolID = %q, want %q", saved.PoolID, "pool-xyz")
	}
}

func TestMirrorContinueNoteCopiesToWorktree(t *testing.T) {
	repoDir := t.TempDir()
	worktreeDir := t.TempDir()
	notesDir := filepath.Join(repoDir, ".prismconductor", "notes")
	if err := os.MkdirAll(notesDir, 0o755); err != nil {
		t.Fatal(err)
	}
	const content = "tests failing in TestFoo — got 404, want 200"
	if err := os.WriteFile(filepath.Join(notesDir, "42.txt"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := mirrorContinueNote(repoDir, worktreeDir, 42); err != nil {
		t.Fatalf("mirrorContinueNote: %v", err)
	}
	got, err := os.ReadFile(filepath.Join(worktreeDir, ".prismconductor", "notes", "42.txt"))
	if err != nil {
		t.Fatalf("read mirrored note: %v", err)
	}
	if string(got) != content {
		t.Errorf("note content = %q, want %q", got, content)
	}
}

func TestMirrorContinueNoteNoopWhenMissing(t *testing.T) {
	repoDir := t.TempDir()
	worktreeDir := t.TempDir()
	// No note file written — mirrorContinueNote must succeed silently.
	if err := mirrorContinueNote(repoDir, worktreeDir, 99); err != nil {
		t.Fatalf("mirrorContinueNote with missing note: %v", err)
	}
	dst := filepath.Join(worktreeDir, ".prismconductor", "notes", "99.txt")
	if _, err := os.Stat(dst); !os.IsNotExist(err) {
		t.Errorf("expected no file at %s, got err=%v", dst, err)
	}
}

func TestExecuteContinuePromptBundledMode(t *testing.T) {
	ws := types.Workspace{
		ID:       "ws-1",
		RepoPath: "/repo",
		SkillProfile: types.SkillProfile{
			Mode: types.SkillModeBundled,
		},
	}
	issue := types.Issue{Number: 80, WorkspaceID: ws.ID}
	plan := types.Plan{Revision: 2}
	got := executeContinuePrompt(ws, issue, plan)
	if !strings.Contains(got, "/conductor-continue") {
		t.Errorf("bundled prompt %q missing /conductor-continue", got)
	}
	if !strings.Contains(got, "--issue 80") {
		t.Errorf("bundled prompt %q missing --issue 80", got)
	}
	if !strings.Contains(got, "--revision 2") {
		t.Errorf("bundled prompt %q missing --revision 2", got)
	}
}
