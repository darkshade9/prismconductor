// Package agentterm manages ephemeral PTY-backed agent CLI sessions.
// One session is allowed per workspace; sessions are never persisted to disk.
package agentterm

import (
	"encoding/base64"
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"sync"

	"github.com/creack/pty"

	"prismconductor/internal/types"
)

// KnownAgentNames is the ordered list of well-known CLI agent binaries to probe.
var KnownAgentNames = []string{"claude", "aider", "gemini", "goose", "continue", "gpt"}

// DiscoverAgents probes PATH for known agent CLI binaries and returns those found.
func DiscoverAgents() []types.AgentInfo {
	var out []types.AgentInfo
	for _, name := range KnownAgentNames {
		if p, err := exec.LookPath(name); err == nil {
			out = append(out, types.AgentInfo{Name: name, Binary: p})
		}
	}
	return out
}

// DataHandler receives base64-encoded raw PTY output bytes.
type DataHandler func(workspaceID, dataB64 string)

// ExitHandler is called when a session's subprocess exits.
type ExitHandler func(workspaceID string, exitCode int)

type ptySess struct {
	cmd  *exec.Cmd
	ptmx *os.File
}

// Manager owns one ephemeral PTY session per workspace.
type Manager struct {
	mu       sync.Mutex
	sessions map[string]*ptySess
	onData   DataHandler
	onExit   ExitHandler
}

// New creates a Manager. onData receives base64-encoded PTY output chunks;
// onExit is called when a session's subprocess exits.
func New(onData DataHandler, onExit ExitHandler) *Manager {
	return &Manager{
		sessions: make(map[string]*ptySess),
		onData:   onData,
		onExit:   onExit,
	}
}

// Start spawns a PTY-backed subprocess for workspaceID, killing any existing
// session first. Returns the child PID.
func (m *Manager) Start(workspaceID, bin string, args []string, cols, rows uint16) (int, error) {
	m.mu.Lock()
	old, hadOld := m.sessions[workspaceID]
	if hadOld {
		delete(m.sessions, workspaceID)
	}
	m.mu.Unlock()

	if hadOld {
		killSess(old)
	}

	cmd := exec.Command(bin, args...)
	cmd.Env = append(os.Environ(), "TERM=xterm-256color")

	ptmx, err := pty.Start(cmd)
	if err != nil {
		return 0, fmt.Errorf("pty start: %w", err)
	}
	if err := pty.Setsize(ptmx, &pty.Winsize{Rows: rows, Cols: cols}); err != nil {
		log.Printf("agentterm: setsize: %v", err)
	}

	sess := &ptySess{cmd: cmd, ptmx: ptmx}
	m.mu.Lock()
	m.sessions[workspaceID] = sess
	m.mu.Unlock()

	go m.readLoop(workspaceID, sess)

	return cmd.Process.Pid, nil
}

func (m *Manager) readLoop(workspaceID string, sess *ptySess) {
	buf := make([]byte, 4096)
	for {
		n, err := sess.ptmx.Read(buf)
		if n > 0 && m.onData != nil {
			m.onData(workspaceID, base64.StdEncoding.EncodeToString(buf[:n]))
		}
		if err != nil {
			if err != io.EOF {
				log.Printf("agentterm: read %s: %v", workspaceID, err)
			}
			break
		}
	}

	exitCode := 0
	if err := sess.cmd.Wait(); err != nil {
		if ee, ok := err.(*exec.ExitError); ok {
			exitCode = ee.ExitCode()
		}
	}

	m.mu.Lock()
	if m.sessions[workspaceID] == sess {
		delete(m.sessions, workspaceID)
	}
	m.mu.Unlock()

	if m.onExit != nil {
		m.onExit(workspaceID, exitCode)
	}
}

// Write sends raw bytes (base64-encoded) to the workspace session's PTY.
func (m *Manager) Write(workspaceID, dataB64 string) error {
	m.mu.Lock()
	sess, ok := m.sessions[workspaceID]
	m.mu.Unlock()
	if !ok {
		return fmt.Errorf("agentterm: no session for workspace %q", workspaceID)
	}
	data, err := base64.StdEncoding.DecodeString(dataB64)
	if err != nil {
		return fmt.Errorf("agentterm: decode input: %w", err)
	}
	_, err = sess.ptmx.Write(data)
	return err
}

// Resize updates the PTY window dimensions.
func (m *Manager) Resize(workspaceID string, cols, rows uint16) error {
	m.mu.Lock()
	sess, ok := m.sessions[workspaceID]
	m.mu.Unlock()
	if !ok {
		return fmt.Errorf("agentterm: no session for workspace %q", workspaceID)
	}
	return pty.Setsize(sess.ptmx, &pty.Winsize{Rows: rows, Cols: cols})
}

// Kill terminates the session for the given workspace.
func (m *Manager) Kill(workspaceID string) error {
	m.mu.Lock()
	sess, ok := m.sessions[workspaceID]
	if ok {
		delete(m.sessions, workspaceID)
	}
	m.mu.Unlock()
	if !ok {
		return fmt.Errorf("agentterm: no session for workspace %q", workspaceID)
	}
	killSess(sess)
	return nil
}

// HasSession reports whether the workspace currently has an active session.
func (m *Manager) HasSession(workspaceID string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	_, ok := m.sessions[workspaceID]
	return ok
}

func killSess(sess *ptySess) {
	_ = sess.ptmx.Close()
	if sess.cmd.Process != nil {
		_ = sess.cmd.Process.Kill()
	}
}
