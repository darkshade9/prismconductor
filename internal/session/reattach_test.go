package session

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"prismconductor/internal/types"
)

// fakePoolRehydrator records AcquireSpecific calls for test assertions.
type fakePoolRehydrator struct {
	acquired []string
}

func (f *fakePoolRehydrator) AcquireSpecific(poolID string) (bool, error) {
	f.acquired = append(f.acquired, poolID)
	return true, nil
}

// writeTranscript writes raw content to a temp transcript file and returns
// its path.
func writeTranscript(t *testing.T, content string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "transcript.log")
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write transcript: %v", err)
	}
	return path
}

// newReattachRuntime builds a runtimeSession suitable for classifyDeadWorker:
// parser disabled (so feedLine treats raw lines as final), PID=0 so pidAlive
// returns false.
func newReattachRuntime(transcriptPath, sessID string, state types.SessionState) *runtimeSession {
	return &runtimeSession{
		sess: &types.Session{
			ID:    sessID,
			State: state,
			PID:   0,
		},
		parser:         &StreamParser{enabled: false},
		transcriptPath: transcriptPath,
		reattached:     true,
	}
}

func TestClassifyDeadWorkerWorkComplete(t *testing.T) {
	path := writeTranscript(t, "starting…\nWork complete.\n")
	store := &fakePersister{}
	m := &Manager{store: store}
	rs := newReattachRuntime(path, "sess-complete", types.StateRunning)
	m.classifyDeadWorker(rs)
	if rs.sess.State != types.StateCompleted {
		t.Fatalf("state = %s, want completed", rs.sess.State)
	}
	if rs.sess.EndedAt == nil {
		t.Errorf("EndedAt should be set on terminal classify")
	}
	if len(store.saves) == 0 {
		t.Errorf("expected SaveSession on dead-worker classify")
	}
}

func TestClassifyDeadWorkerBlocked(t *testing.T) {
	path := writeTranscript(t, "BLOCKED: tests failed\n")
	store := &fakePersister{}
	m := &Manager{store: store}
	rs := newReattachRuntime(path, "sess-blocked", types.StateRunning)
	m.classifyDeadWorker(rs)
	if rs.sess.State != types.StateBlocked {
		t.Fatalf("state = %s, want blocked", rs.sess.State)
	}
	if rs.sess.BlockedReason != "tests failed" {
		t.Errorf("BlockedReason = %q, want %q", rs.sess.BlockedReason, "tests failed")
	}
}

func TestClassifyDeadWorkerNoSentinelMarksFailed(t *testing.T) {
	// Worker died with no terminal sentinel — q6: mark failed with explicit
	// reason so the user can re-approve.
	path := writeTranscript(t, "@tool Edit\n@asst something happening\n")
	store := &fakePersister{}
	m := &Manager{store: store}
	rs := newReattachRuntime(path, "sess-nope", types.StateRunning)
	m.classifyDeadWorker(rs)
	if rs.sess.State != types.StateFailed {
		t.Fatalf("state = %s, want failed", rs.sess.State)
	}
	if rs.sess.BlockedReason == "" {
		t.Errorf("BlockedReason should be populated for no-sentinel failure")
	}
}

func TestClassifyDeadWorkerFiresPROpenedRetroactive(t *testing.T) {
	// Q5: a worker that emitted PR_OPENED before dying must trigger
	// onPROpened on re-attach so the card moves to REVIEW. The state
	// machine itself stays in whatever the next sentinel sets — if the
	// worker also emitted Work complete., state=completed; otherwise state
	// stays running and the q6 arm bumps to failed. Either way the
	// onPROpened side effect must fire exactly once.
	path := writeTranscript(t, "PR_OPENED: https://github.com/o/r/pull/42\nWork complete.\n")
	store := &fakePersister{}
	var fired []string
	m := &Manager{
		store: store,
		onPROpened: func(_ types.Session, url string) {
			fired = append(fired, url)
		},
	}
	rs := newReattachRuntime(path, "sess-pr", types.StateRunning)
	m.classifyDeadWorker(rs)
	if len(fired) != 1 {
		t.Fatalf("onPROpened fire count = %d, want 1 (urls=%v)", len(fired), fired)
	}
	if fired[0] != "https://github.com/o/r/pull/42" {
		t.Errorf("onPROpened url = %q, want pull/42", fired[0])
	}
	if rs.sess.State != types.StateCompleted {
		t.Errorf("state = %s, want completed (followed by Work complete)", rs.sess.State)
	}
}

func TestClassifyDeadWorkerKeepsPausedForQuestion(t *testing.T) {
	// QUESTION_PENDING → paused state. matchPatterns sets PendingQuestionID;
	// classifyDeadWorker must not overwrite it with StateFailed.
	path := writeTranscript(t, "QUESTION_PENDING: 11111111-2222-3333-4444-555555555555\n")
	store := &fakePersister{}
	m := &Manager{store: store}
	rs := newReattachRuntime(path, "sess-paused", types.StateRunning)
	m.classifyDeadWorker(rs)
	if rs.sess.State != types.StatePausedForQuestion {
		t.Fatalf("state = %s, want paused_for_question", rs.sess.State)
	}
	if rs.sess.PendingQuestionID == "" {
		t.Errorf("PendingQuestionID empty, expected the captured uuid")
	}
}

// TestReattach_LiveSessionAcquiresPoolSlot verifies that Reattach calls
// AcquireSpecific on the pool rehydrator for every live session that carries
// a non-empty PoolID. This restores the in-memory active counter after a
// conductor restart so the UI worker counter does not undercount.
func TestReattach_LiveSessionAcquiresPoolSlot(t *testing.T) {
	// Write an empty transcript so the follow goroutine can open it.
	dir := t.TempDir()
	transcriptPath := filepath.Join(dir, "live.log")
	if err := os.WriteFile(transcriptPath, []byte(""), 0o644); err != nil {
		t.Fatalf("write empty transcript: %v", err)
	}

	reg := &fakePoolRehydrator{}
	m := &Manager{
		sessions: map[string]*runtimeSession{},
		store:    &fakePersister{},
		poolReg:  reg,
	}

	sess := types.Session{
		ID:          "live-sess",
		PoolID:      "pool-1",
		State:       types.StateRunning,
		PID:         os.Getpid(), // current process is always alive
		Transcript:  transcriptPath,
		StartedAt:   time.Now(),
	}

	m.Reattach([]types.Session{sess}, nil)

	// AcquireSpecific is called synchronously before the follow goroutine starts.
	if len(reg.acquired) != 1 || reg.acquired[0] != "pool-1" {
		t.Errorf("AcquireSpecific calls = %v, want [pool-1]", reg.acquired)
	}

	// Clean up the goroutine that was started for the live session.
	m.mu.Lock()
	if rs, ok := m.sessions["live-sess"]; ok && rs.cancel != nil {
		rs.cancel()
	}
	m.mu.Unlock()
}

// TestReattach_DeadSessionSkipsAcquire verifies that AcquireSpecific is NOT
// called for sessions whose worker is already dead on arrival (PID=0).
func TestReattach_DeadSessionSkipsAcquire(t *testing.T) {
	path := writeTranscript(t, "Work complete.\n")

	reg := &fakePoolRehydrator{}
	m := &Manager{
		sessions: map[string]*runtimeSession{},
		store:    &fakePersister{},
		poolReg:  reg,
	}

	sess := types.Session{
		ID:         "dead-sess",
		PoolID:     "pool-1",
		State:      types.StateRunning,
		PID:        0, // dead
		Transcript: path,
	}

	m.Reattach([]types.Session{sess}, nil)

	if len(reg.acquired) != 0 {
		t.Errorf("AcquireSpecific called %d times for dead session, want 0", len(reg.acquired))
	}
}

func TestClassifyDeadWorkerHonorsTranscriptOffset(t *testing.T) {
	// Bytes BEFORE the offset must NOT be re-fed (would emit duplicate
	// onPROpened and corrupt activity counters). Plant a sentinel before
	// the offset and a benign line after; assert no PR_OPENED fire.
	preLine := "PR_OPENED: https://github.com/o/r/pull/9\n"
	path := writeTranscript(t, preLine+"@asst hi\n")
	store := &fakePersister{}
	var fired int
	m := &Manager{
		store:      store,
		onPROpened: func(_ types.Session, _ string) { fired++ },
	}
	rs := newReattachRuntime(path, "sess-offset", types.StateRunning)
	rs.transcriptOffset = int64(len(preLine))
	m.classifyDeadWorker(rs)
	if fired != 0 {
		t.Errorf("onPROpened fired %d times, want 0 (sentinel was below offset)", fired)
	}
}
