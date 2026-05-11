package session

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

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

// TestSpawnPlan_RemoteWorkspace_ReturnsPaused verifies that SpawnPlan returns
// ErrRemoteWorkspacePaused for any remote workspace, regardless of config (#254).
func TestSpawnPlan_RemoteWorkspace_ReturnsPaused(t *testing.T) {
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
		{
			name: "fully configured remote",
			ws: types.Workspace{
				ID:              "ws-remote-configured",
				ExecutionTarget: types.ExecutionTargetRemote,
				RemoteConfig:    &types.RemoteConfig{CFWorkerEndpointURL: "https://worker.example.com"},
			},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			m := buildTestManager(t)
			_, err := m.SpawnPlan(tc.ws, types.Issue{Number: 1, WorkspaceID: tc.ws.ID}, types.Pool{})
			if !errors.Is(err, ErrRemoteWorkspacePaused) {
				t.Errorf("want ErrRemoteWorkspacePaused, got %v", err)
			}
		})
	}
}

// TestSpawnExecute_RemoteWorkspace_ReturnsPaused verifies that SpawnExecute also
// returns ErrRemoteWorkspacePaused for any remote workspace (#254).
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
// SpawnExecuteResume returns ErrRemoteWorkspacePaused for any remote workspace (#254).
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

// TestSpawnPlan_RemoteWorkspace_NoHTTPCall verifies that a paused remote
// workspace does not make any HTTP call to its configured endpoint (#254).
func TestSpawnPlan_RemoteWorkspace_NoHTTPCall(t *testing.T) {
	httpCalled := false
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		httpCalled = true
		http.Error(w, "should not be called", 500)
	}))
	defer srv.Close()

	ws := types.Workspace{
		ID:              "ws-remote-no-http",
		ExecutionTarget: types.ExecutionTargetRemote,
		RemoteConfig:    &types.RemoteConfig{CFWorkerEndpointURL: srv.URL},
	}
	m := buildTestManager(t)
	_, err := m.SpawnPlan(ws, types.Issue{Number: 1, WorkspaceID: ws.ID}, types.Pool{})
	if !errors.Is(err, ErrRemoteWorkspacePaused) {
		t.Fatalf("want ErrRemoteWorkspacePaused, got %v", err)
	}
	if httpCalled {
		t.Error("SpawnPlan on a paused remote workspace must not make any HTTP call")
	}
}
