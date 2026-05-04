package agentterm_test

import (
	"encoding/base64"
	"sync"
	"testing"
	"time"

	"prismconductor/internal/agentterm"
	"prismconductor/internal/types"
)

func TestDiscoverAgents(t *testing.T) {
	agents := agentterm.DiscoverAgents()
	for _, a := range agents {
		if a.Name == "" {
			t.Errorf("agent with empty name: %+v", a)
		}
		if a.Binary == "" {
			t.Errorf("agent %q has empty binary path", a.Name)
		}
	}
}

func TestManagerStartAndOutput(t *testing.T) {
	var mu sync.Mutex
	var received []byte

	mgr := agentterm.New(
		func(_ string, dataB64 string) {
			data, _ := base64.StdEncoding.DecodeString(dataB64)
			mu.Lock()
			received = append(received, data...)
			mu.Unlock()
		},
		nil,
	)

	pid, err := mgr.Start("ws1", "echo", []string{"hello"}, 80, 24)
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	if pid <= 0 {
		t.Errorf("expected positive PID, got %d", pid)
	}

	// Give the process time to write output and exit.
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		mu.Lock()
		n := len(received)
		mu.Unlock()
		if n > 0 {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}

	mu.Lock()
	got := string(received)
	mu.Unlock()
	if got == "" {
		t.Error("expected PTY output but got none")
	}
}

func TestManagerExitCallback(t *testing.T) {
	done := make(chan int, 1)

	mgr := agentterm.New(
		nil,
		func(_ string, code int) { done <- code },
	)

	if _, err := mgr.Start("ws2", "true", nil, 80, 24); err != nil {
		t.Fatalf("Start: %v", err)
	}

	select {
	case code := <-done:
		if code != 0 {
			t.Errorf("expected exit code 0, got %d", code)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for exit callback")
	}
}

func TestManagerWriteNoSession(t *testing.T) {
	mgr := agentterm.New(nil, nil)
	err := mgr.Write("nonexistent", base64.StdEncoding.EncodeToString([]byte("hi")))
	if err == nil {
		t.Error("expected error writing to nonexistent session")
	}
}

func TestManagerKillNoSession(t *testing.T) {
	mgr := agentterm.New(nil, nil)
	if err := mgr.Kill("nonexistent"); err == nil {
		t.Error("expected error killing nonexistent session")
	}
}

func TestManagerResizeNoSession(t *testing.T) {
	mgr := agentterm.New(nil, nil)
	if err := mgr.Resize("nonexistent", 80, 24); err == nil {
		t.Error("expected error resizing nonexistent session")
	}
}

func TestManagerKill(t *testing.T) {
	mgr := agentterm.New(nil, nil)

	if _, err := mgr.Start("ws3", "cat", nil, 80, 24); err != nil {
		t.Fatalf("Start: %v", err)
	}
	if !mgr.HasSession("ws3") {
		t.Fatal("expected session after Start")
	}
	if err := mgr.Kill("ws3"); err != nil {
		t.Fatalf("Kill: %v", err)
	}
	// After Kill the in-memory entry is removed immediately.
	if mgr.HasSession("ws3") {
		t.Error("expected session gone after Kill")
	}
}

// TestAgentInfoShape is a compile-time shape check.
func TestAgentInfoShape(_ *testing.T) {
	_ = types.AgentInfo{Name: "claude", Binary: "/usr/local/bin/claude"}
	_ = types.AgentTermSession{WorkspaceID: "ws1", SessionID: "s1", AgentBin: "claude", PID: 123}
}
