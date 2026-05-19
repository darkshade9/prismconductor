package session

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"prismconductor/internal/remoteworker"
	"prismconductor/internal/types"
)

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
		RemoteConfig:    &types.RemoteConfig{CFWorkerEndpointURL: srv.URL},
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

// TestSpawnPlan_RemoteWorkspace_NoEndpoint verifies ErrRemoteNotReady when the
// workspace has no CFWorkerEndpointURL configured (issue #284).
func TestSpawnPlan_RemoteWorkspace_NoEndpoint(t *testing.T) {
	cases := []struct {
		name string
		ws   types.Workspace
	}{
		{
			name: "nil RemoteConfig",
			ws: types.Workspace{
				ID:              "ws-remote-no-rc",
				ExecutionTarget: types.ExecutionTargetRemote,
				RemoteConfig:    nil,
			},
		},
		{
			name: "empty CFWorkerEndpointURL",
			ws: types.Workspace{
				ID:              "ws-remote-no-url",
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

// TestSpawnPlan_RemoteWorkspace_CallsEndpoint verifies that SpawnPlan routes a
// remote workspace to the configured endpoint (issue #284 Phase 1: sandbox).
func TestSpawnPlan_RemoteWorkspace_CallsEndpoint(t *testing.T) {
	called := false
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		// Respond as a real sandbox worker would on POST /sessions.
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		w.Write([]byte(`{"session_id":"test-session-id"}`))
	}))
	defer srv.Close()

	wsID := "ws-remote-configured"

	// Set up a test conductor API key so spawnRemotePlan can find it (it reads
	// from the keychain before making the HTTP call).
	keychainDir := t.TempDir()
	restore := remoteworker.OverrideKeychainDir(keychainDir)
	defer restore()
	if _, err := remoteworker.SetKey(wsID, "test-api-key"); err != nil {
		t.Fatalf("SetKey: %v", err)
	}

	ws := types.Workspace{
		ID:              wsID,
		ExecutionTarget: types.ExecutionTargetRemote,
		RemoteConfig:    &types.RemoteConfig{CFWorkerEndpointURL: srv.URL},
	}
	m := buildTestManager(t)

	// The remote call will succeed but the SSE stream won't have data; the
	// session is created and a goroutine starts. We only care that the HTTP
	// call was made (not ErrRemoteWorkspacePaused or ErrRemoteNotReady).
	_, err := m.SpawnPlan(ws, types.Issue{Number: 1, WorkspaceID: ws.ID}, types.Pool{})
	if errors.Is(err, ErrRemoteWorkspacePaused) {
		t.Fatal("SpawnPlan on a configured remote workspace must not return ErrRemoteWorkspacePaused (Phase 1 sandbox is active)")
	}
	if errors.Is(err, ErrRemoteNotReady) {
		t.Fatal("SpawnPlan with a configured endpoint must not return ErrRemoteNotReady")
	}
	if !called {
		t.Error("SpawnPlan on a configured remote workspace must make an HTTP call to the endpoint")
	}
}

// TestSpawnExecute_RemoteWorkspace_ReturnsPaused verifies that SpawnExecute
// still returns ErrRemoteWorkspacePaused for remote workspaces (execute path
// opens in Phase 2, not Phase 1).
func TestSpawnExecute_RemoteWorkspace_ReturnsPaused(t *testing.T) {
	ws := types.Workspace{
		ID:              "ws-remote-exec",
		ExecutionTarget: types.ExecutionTargetRemote,
		RemoteConfig:    &types.RemoteConfig{CFWorkerEndpointURL: "https://worker.example.com"},
	}
	m := buildTestManager(t)
	_, err := m.SpawnExecute(ws, types.Issue{Number: 2, WorkspaceID: ws.ID}, types.Plan{}, types.Pool{})
	if !errors.Is(err, ErrRemoteWorkspacePaused) {
		t.Errorf("SpawnExecute: want ErrRemoteWorkspacePaused, got %v", err)
	}
}

// TestSpawnExecuteResume_RemoteWorkspace_ReturnsPaused verifies that
// SpawnExecuteResume still returns ErrRemoteWorkspacePaused (Phase 2).
func TestSpawnExecuteResume_RemoteWorkspace_ReturnsPaused(t *testing.T) {
	ws := types.Workspace{
		ID:              "ws-remote-resume",
		ExecutionTarget: types.ExecutionTargetRemote,
		RemoteConfig:    &types.RemoteConfig{CFWorkerEndpointURL: "https://worker.example.com"},
	}
	m := buildTestManager(t)
	_, err := m.SpawnExecuteResume(ws, types.Issue{Number: 3, WorkspaceID: ws.ID}, types.Plan{}, types.Pool{}, "q-123")
	if !errors.Is(err, ErrRemoteWorkspacePaused) {
		t.Errorf("SpawnExecuteResume: want ErrRemoteWorkspacePaused, got %v", err)
	}
}
