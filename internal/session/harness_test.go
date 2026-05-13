package session

import (
	"context"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"prismconductor/internal/llm"
	"prismconductor/internal/types"
)

// controlledProviderKind is the types.Provider key for the test fake.
const controlledProviderKind types.Provider = "fake-harness-test"

// controlledProvider is a programmable llm.Provider used to exercise harness
// lifecycle paths without real HTTP. Set behavior to:
//
//   - "panic"  — ToolChat panics with a known message.
//   - "hang"   — ToolChat blocks until ctx is cancelled.
//   - "stop"   — ToolChat immediately returns Stop: true (clean exit).
type controlledProvider struct {
	behavior string
}

func (controlledProvider) Kind() types.Provider    { return controlledProviderKind }
func (controlledProvider) DisplayName() string     { return "fake-harness-test" }
func (controlledProvider) DefaultEndpoint() string { return "" }
func (controlledProvider) NeedsAPIKey() bool       { return false }
func (controlledProvider) APIKeyHelpURL() string   { return "" }
func (controlledProvider) CanSpawn() bool          { return true }
func (controlledProvider) ListModels(context.Context, types.Pool) ([]string, error) {
	return nil, llm.ErrNotSupported
}
func (controlledProvider) SpawnArgs(types.Pool, string) ([]string, error) {
	return nil, llm.ErrNotSupported
}
func (controlledProvider) ChatJSON(context.Context, types.Pool, string, string) (string, error) {
	return "", llm.ErrNotSupported
}
func (p controlledProvider) ToolChat(ctx context.Context, _ types.Pool, _ llm.ChatRequest) (llm.ChatResponse, error) {
	switch p.behavior {
	case "panic":
		panic("injected test panic")
	case "hang":
		<-ctx.Done()
		return llm.ChatResponse{}, ctx.Err()
	default: // "stop"
		return llm.ChatResponse{Stop: true}, nil
	}
}

// spawnHarnessSession is a helper that registers a fake harness provider, spawns
// a harness-strategy session, and returns the session plus the manager for
// assertion. The caller is responsible for cleanup via m.Kill if needed.
func spawnHarnessSession(t *testing.T, behavior string) (sess *types.Session, m *Manager, store *fakePersister) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("POSIX-only: harness integration tests require os.Pipe / file I/O")
	}
	tdir := t.TempDir()
	store = &fakePersister{}
	m = NewManager(nil, nil)
	m.Configure(filepath.Join(tdir, "transcripts"), store, nil)
	m.SetProviders(llm.NewRegistry(controlledProvider{behavior: behavior}))

	ws := types.Workspace{ID: "ws-h", RepoPath: tdir}
	issue := types.Issue{Number: 501, WorkspaceID: ws.ID}
	pool := types.Pool{ID: "pool-h", Provider: controlledProviderKind}

	var err error
	sess, err = m.spawnWithDir(ws, issue, types.ModePlan, nil, "test prompt", "", "", pool, "")
	if err != nil {
		t.Fatalf("spawnWithDir: %v", err)
	}
	return sess, m, store
}

// waitForSessionGone polls m.sessions until the session is removed or a
// deadline expires. Returns true when the session is gone.
func waitForSessionGone(m *Manager, sessID string, timeout time.Duration) bool {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		m.mu.RLock()
		_, there := m.sessions[sessID]
		m.mu.RUnlock()
		if !there {
			return true
		}
		time.Sleep(20 * time.Millisecond)
	}
	return false
}

// lastStateFor returns the last state update recorded by fakePersister for the
// given session ID, or "" if none was recorded.
func lastStateFor(store *fakePersister, sessID string) string {
	var last string
	for _, upd := range store.stateUpd {
		if strings.HasPrefix(upd, sessID+":") {
			last = strings.TrimPrefix(upd, sessID+":")
		}
	}
	return last
}

// TestHarnessPanicLandsInFailedState verifies that a panic inside
// harness.Execute is caught by the spawnHarness recovery defer, written to
// rs.harnessErr, and then mapped to state=failed by mapTerminalState via the
// normal tailAndParse path.
func TestHarnessPanicLandsInFailedState(t *testing.T) {
	sess, m, store := spawnHarnessSession(t, "panic")

	if !waitForSessionGone(m, sess.ID, 5*time.Second) {
		t.Fatal("session never exited after harness panic")
	}

	got := lastStateFor(store, sess.ID)
	if got != "failed" {
		t.Errorf("state after panic = %q, want failed; all updates=%v", got, store.stateUpd)
	}
}

// TestHarnessContextCancelLandsInFailedState verifies that cancelling the
// session context (via Kill) results in state=failed (context.Canceled from
// harness.Execute → mapTerminalState → StateFailed).
func TestHarnessContextCancelLandsInFailedState(t *testing.T) {
	sess, m, store := spawnHarnessSession(t, "hang")

	// Give the goroutine time to enter the blocking ToolChat.
	time.Sleep(50 * time.Millisecond)

	if err := m.Kill(sess.ID); err != nil {
		t.Fatalf("Kill: %v", err)
	}

	if !waitForSessionGone(m, sess.ID, 5*time.Second) {
		t.Fatal("session never exited after Kill")
	}

	got := lastStateFor(store, sess.ID)
	if got != "failed" {
		t.Errorf("state after Kill = %q, want failed; all updates=%v", got, store.stateUpd)
	}
}

// TestHarnessCleanExitLandsInCompletedState verifies the happy path: a harness
// that returns immediately with Stop:true ends with state=completed.
func TestHarnessCleanExitLandsInCompletedState(t *testing.T) {
	sess, m, store := spawnHarnessSession(t, "stop")

	if !waitForSessionGone(m, sess.ID, 5*time.Second) {
		t.Fatal("session never exited after clean stop")
	}

	got := lastStateFor(store, sess.ID)
	if got != "completed" {
		t.Errorf("state after clean stop = %q, want completed; all updates=%v", got, store.stateUpd)
	}
}

// TestHarnessKillRemovesFromSessionsMap verifies that Kill + session exit
// removes the entry from Manager.sessions so subsequent Snapshot calls don't
// return a zombie running session.
func TestHarnessKillRemovesFromSessionsMap(t *testing.T) {
	sess, m, _ := spawnHarnessSession(t, "hang")

	time.Sleep(50 * time.Millisecond)
	if err := m.Kill(sess.ID); err != nil {
		t.Fatalf("Kill: %v", err)
	}

	if !waitForSessionGone(m, sess.ID, 5*time.Second) {
		t.Fatal("session still in m.sessions after Kill + exit deadline")
	}

	// Snapshot must not return the killed session.
	for _, s := range m.Snapshot() {
		if s.ID == sess.ID {
			t.Errorf("Snapshot returned killed session %s", sess.ID)
		}
	}
}

// TestWatchdogScanKillsStaleSession exercises runWatchdogScan directly with a
// fabricated stale lastLineAt. The session is a real harness session in "hang"
// mode; we backdate lastLineAt past WatchdogTimeout and verify that the
// watchdog issues a Kill that results in a terminal state.
func TestWatchdogScanKillsStaleSession(t *testing.T) {
	sess, m, store := spawnHarnessSession(t, "hang")

	// Backdate lastLineAt so the watchdog considers the session stale.
	m.mu.RLock()
	rs := m.sessions[sess.ID]
	m.mu.RUnlock()
	if rs == nil {
		t.Fatal("session not in map before watchdog scan")
	}
	rs.actMu.Lock()
	rs.lastLineAt = time.Now().Add(-(WatchdogTimeout + time.Second))
	rs.actMu.Unlock()

	m.runWatchdogScan()

	if !waitForSessionGone(m, sess.ID, 5*time.Second) {
		t.Fatal("session never exited after watchdog kill")
	}

	got := lastStateFor(store, sess.ID)
	if got != "failed" {
		t.Errorf("state after watchdog kill = %q, want failed; updates=%v", got, store.stateUpd)
	}
}
