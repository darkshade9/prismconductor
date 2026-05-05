package session

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"prismconductor/internal/remoteworker"
	"prismconductor/internal/types"
)

// swapHTTPClient replaces the package-level http client inside remoteworker
// for the duration of a test via the test-helper exported by that package.
// We can't call replaceHTTPClient directly (it's in a different package and
// test-only), so we use the same httptest.Server trick that the remote-worker
// tests use: pass the server's client to the equivalent test-helper.
//
// This test file directly instantiates a Manager and exercises SpawnPlan;
// it uses a real httptest.Server whose URL is wired into the workspace
// RemoteConfig, bypassing the package-level http.Client substitution by
// relying on the default http.Client pointing at the test server's loopback.

// buildTestManager returns a minimal Manager configured for test spawns.
func buildTestManager(t *testing.T) *Manager {
	t.Helper()
	dir := t.TempDir()
	m := &Manager{
		sessions:      make(map[string]*runtimeSession),
		inFlight:      make(map[string]bool),
		transcriptDir: dir,
	}
	return m
}

// TestSpawnPlan_LocalWorkspace_DoesNotCallRemote verifies that SpawnPlan does
// not try to call any remote endpoint when ExecutionTarget is empty (local).
func TestSpawnPlan_LocalWorkspace_DoesNotCallRemote(t *testing.T) {
	remoteCallMade := false
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		remoteCallMade = true
		http.Error(w, "should not be called", 500)
	}))
	defer srv.Close()

	ws := types.Workspace{
		ID:              "ws-local",
		ExecutionTarget: types.ExecutionTargetLocal,
		// No RemoteConfig — but even if present, local target must not call it.
		RemoteConfig: &types.RemoteConfig{CFWorkerEndpointURL: srv.URL},
	}
	issue := types.Issue{Number: 1, WorkspaceID: ws.ID}
	pool := types.Pool{}

	m := buildTestManager(t)
	// SpawnPlan for a local workspace will call buildPlanCommand which needs
	// providers; it fails fast with a provider error — that's fine. What
	// matters is no HTTP call was made to the test server.
	_, _ = m.SpawnPlan(ws, issue, pool)

	if remoteCallMade {
		t.Error("SpawnPlan on a local workspace must not make any HTTP call to a remote endpoint")
	}
}

// TestSpawnPlan_RemoteWorkspace_NoConfig_ReturnsErrRemoteNotReady verifies
// that SpawnPlan returns ErrRemoteNotReady immediately when ExecutionTarget
// is remote but RemoteConfig or CFWorkerEndpointURL is missing.
func TestSpawnPlan_RemoteWorkspace_NoConfig_ReturnsErrRemoteNotReady(t *testing.T) {
	cases := []struct {
		name string
		ws   types.Workspace
	}{
		{
			name: "nil RemoteConfig",
			ws: types.Workspace{
				ID:              "ws-no-rc",
				ExecutionTarget: types.ExecutionTargetRemote,
				RemoteConfig:    nil,
			},
		},
		{
			name: "empty CFWorkerEndpointURL",
			ws: types.Workspace{
				ID:              "ws-no-url",
				ExecutionTarget: types.ExecutionTargetRemote,
				RemoteConfig:    &types.RemoteConfig{CFWorkerEndpointURL: ""},
			},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			m := buildTestManager(t)
			_, err := m.SpawnPlan(tc.ws, types.Issue{Number: 1, WorkspaceID: tc.ws.ID}, types.Pool{})
			if !errors.Is(err, ErrRemoteNotReady) {
				t.Errorf("want ErrRemoteNotReady, got %v", err)
			}
		})
	}
}

// TestSpawnPlan_RemoteWorkspace_RoutesToRemote verifies that SpawnPlan posts
// to the remote worker's /sessions endpoint with mode="plan" when
// ExecutionTarget is remote and the endpoint is configured.
func TestSpawnPlan_RemoteWorkspace_RoutesToRemote(t *testing.T) {
	sessionID := "plan-sess-001"
	var gotBody map[string]any
	var gotPath string

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		switch {
		case r.Method == "POST" && r.URL.Path == "/sessions":
			_ = json.NewDecoder(r.Body).Decode(&gotBody)
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusCreated)
			json.NewEncoder(w).Encode(remoteworker.SpawnResponse{SessionID: sessionID})

		case r.Method == "GET" && r.URL.Path == "/sessions/"+sessionID+"/stream":
			// Return empty SSE stream so the goroutine exits cleanly.
			w.Header().Set("Content-Type", "text/event-stream")

		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	// Write a dummy API key file so remoteworker.GetKey succeeds.
	keyDir, err := os.UserConfigDir()
	if err != nil {
		t.Skipf("cannot locate config dir: %v", err)
	}
	keyPath := fmt.Sprintf("%s/PrismConductor/secrets/ws-remote-plan.key", keyDir)
	_ = os.MkdirAll(keyPath[:len(keyPath)-len("/ws-remote-plan.key")], 0o700)
	_ = os.WriteFile(keyPath, []byte("test-api-key"), 0o600)
	t.Cleanup(func() { _ = os.Remove(keyPath) })

	ws := types.Workspace{
		ID:              "ws-remote-plan",
		ExecutionTarget: types.ExecutionTargetRemote,
		RemoteConfig:    &types.RemoteConfig{CFWorkerEndpointURL: srv.URL},
		GitHubOwner:     "acme",
		GitHubRepo:      "myrepo",
	}
	issue := types.Issue{Number: 7, WorkspaceID: ws.ID}
	pool := types.Pool{Model: "claude-opus-4-7"}

	m := buildTestManager(t)
	sess, err := m.SpawnPlan(ws, issue, pool)
	if err != nil {
		t.Fatalf("SpawnPlan: %v", err)
	}
	if sess == nil {
		t.Fatal("expected non-nil session")
	}
	if sess.Mode != types.ModePlan {
		t.Errorf("session mode = %q, want plan", sess.Mode)
	}
	if sess.State != types.StateRunning {
		t.Errorf("session state = %q, want running", sess.State)
	}

	// Wait briefly for the SSE goroutine to make the initial HTTP call.
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if gotPath != "" {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}

	if gotPath != "/sessions" {
		t.Errorf("request path = %q, want /sessions", gotPath)
	}
	if got, ok := gotBody["mode"].(string); !ok || got != "plan" {
		t.Errorf("request body mode = %v, want \"plan\"", gotBody["mode"])
	}
	if got, ok := gotBody["issue_number"].(float64); !ok || int(got) != 7 {
		t.Errorf("request body issue_number = %v, want 7", gotBody["issue_number"])
	}
}

// TestSpawnPlan_RemoteWorkspace_DuplicateGuard verifies that a second SpawnPlan
// call for the same workspace+issue returns ErrDuplicateSpawn when the first
// session is still running.
func TestSpawnPlan_RemoteWorkspace_DuplicateGuard(t *testing.T) {
	sessionID := "plan-dup-001"
	block := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == "POST" && r.URL.Path == "/sessions":
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusCreated)
			json.NewEncoder(w).Encode(remoteworker.SpawnResponse{SessionID: sessionID})
		case r.Method == "GET":
			// Block until test ends so the session stays running.
			w.Header().Set("Content-Type", "text/event-stream")
			<-block
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(func() { close(block); srv.Close() })

	keyDir, _ := os.UserConfigDir()
	keyPath := fmt.Sprintf("%s/PrismConductor/secrets/ws-dup-guard.key", keyDir)
	_ = os.MkdirAll(keyPath[:len(keyPath)-len("/ws-dup-guard.key")], 0o700)
	_ = os.WriteFile(keyPath, []byte("test-key"), 0o600)
	t.Cleanup(func() { _ = os.Remove(keyPath) })

	ws := types.Workspace{
		ID:              "ws-dup-guard",
		ExecutionTarget: types.ExecutionTargetRemote,
		RemoteConfig:    &types.RemoteConfig{CFWorkerEndpointURL: srv.URL},
	}
	issue := types.Issue{Number: 3, WorkspaceID: ws.ID}

	m := buildTestManager(t)
	sess1, err := m.SpawnPlan(ws, issue, types.Pool{})
	if err != nil {
		t.Fatalf("first SpawnPlan: %v", err)
	}
	// Ensure the session is registered as running before the second call.
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		m.mu.Lock()
		rs, ok := m.sessions[sess1.ID]
		m.mu.Unlock()
		if ok && rs != nil {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}

	_, err = m.SpawnPlan(ws, issue, types.Pool{})
	if !errors.Is(err, ErrDuplicateSpawn) {
		t.Errorf("second SpawnPlan: want ErrDuplicateSpawn, got %v", err)
	}
}
