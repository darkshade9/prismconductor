package session

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/creack/pty"
	"github.com/google/uuid"

	"prismconductor/internal/eventbus"
	"prismconductor/internal/types"
)

// LineHandler receives each PTY output line as it arrives.
type LineHandler func(sessionID string, line string)

// Persister is the slice of *store.Store that the session manager needs.
// Defined as an interface so the package doesn't depend on store directly.
type Persister interface {
	SaveSession(sess *types.Session, transcriptPath string) error
	UpdateSessionState(id string, state types.SessionState) error
}

// StateChangeHandler is fired on every state transition. Used by the App layer
// to fan out OS notifications + Wails events.
type StateChangeHandler func(sess types.Session, prev types.SessionState)

// PlanReadyHandler is fired when the worker prints the §10.3 "Plan written"
// sentinel line. The handler is responsible for reading the file off disk and
// persisting it.
type PlanReadyHandler func(sess types.Session, planPath string)

type Manager struct {
	bus           *eventbus.Bus
	emit          LineHandler
	transcriptDir string
	store         Persister
	onStateChange StateChangeHandler
	onPlanReady   PlanReadyHandler

	mu       sync.RWMutex
	sessions map[string]*runtimeSession
}

type runtimeSession struct {
	sess           *types.Session
	cmd            *exec.Cmd
	pty            *os.File
	cancel         context.CancelFunc
	transcriptPath string
	transcriptFile *os.File
	parser         *StreamParser
}

func NewManager(bus *eventbus.Bus, emit LineHandler) *Manager {
	return &Manager{bus: bus, emit: emit, sessions: map[string]*runtimeSession{}}
}

// Configure wires optional persistence + state-change side effects.
func (m *Manager) Configure(transcriptDir string, store Persister, onChange StateChangeHandler) {
	m.transcriptDir = transcriptDir
	m.store = store
	m.onStateChange = onChange
}

// SetOnPlanReady registers the handler invoked when a worker prints the
// "Plan written" sentinel.
func (m *Manager) SetOnPlanReady(h PlanReadyHandler) { m.onPlanReady = h }

// SpawnPlan launches a plan-mode worker per §10.1 / §10.4.
func (m *Manager) SpawnPlan(ws types.Workspace, issue types.Issue) (*types.Session, error) {
	args := buildPlanCommand(ws, issue)
	return m.spawn(ws, issue, types.ModePlan, args)
}

// SpawnExecute launches an execute-mode worker per §10.2.
func (m *Manager) SpawnExecute(ws types.Workspace, issue types.Issue, plan types.Plan) (*types.Session, error) {
	args := buildExecuteCommand(ws, issue, plan)
	return m.spawn(ws, issue, types.ModeExecute, args)
}

// SpawnRaw runs a non-skill command via PTY (used by the day-1 demo: `claude --version`).
func (m *Manager) SpawnRaw(ws types.Workspace, name string, args []string) (*types.Session, error) {
	demoIssue := types.Issue{Number: 0, WorkspaceID: ws.ID}
	full := append([]string{name}, args...)
	return m.spawn(ws, demoIssue, types.ModePlan, full)
}

func (m *Manager) spawn(ws types.Workspace, issue types.Issue, mode types.SessionMode, argv []string) (*types.Session, error) {
	if len(argv) == 0 {
		return nil, fmt.Errorf("empty command")
	}
	cmd := exec.Command(argv[0], argv[1:]...)
	if ws.RepoPath != "" {
		cmd.Dir = ws.RepoPath
	}
	cmd.Env = append(os.Environ(), envSpecToSlice(ws.AgentEnv)...)

	f, err := pty.Start(cmd)
	if err != nil {
		return nil, fmt.Errorf("pty.Start: %w", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	sess := &types.Session{
		ID:          uuid.NewString(),
		WorkspaceID: ws.ID,
		IssueNumber: issue.Number,
		Mode:        mode,
		State:       types.StateRunning,
		StartedAt:   time.Now(),
		PID:         cmd.Process.Pid,
	}

	rs := &runtimeSession{sess: sess, cmd: cmd, pty: f, cancel: cancel, parser: NewStreamParser()}

	if m.transcriptDir != "" {
		_ = os.MkdirAll(m.transcriptDir, 0o755)
		rs.transcriptPath = filepath.Join(m.transcriptDir, sess.ID+".log")
		if tf, err := os.Create(rs.transcriptPath); err == nil {
			rs.transcriptFile = tf
		}
	}
	sess.Transcript = rs.transcriptPath

	if m.store != nil {
		_ = m.store.SaveSession(sess, rs.transcriptPath)
	}
	if m.onStateChange != nil {
		m.onStateChange(*sess, "")
	}

	m.mu.Lock()
	m.sessions[sess.ID] = rs
	m.mu.Unlock()

	go m.tailAndParse(ctx, rs)
	return sess, nil
}

func (m *Manager) tailAndParse(ctx context.Context, rs *runtimeSession) {
	defer func() {
		rs.pty.Close()
		if rs.transcriptFile != nil {
			rs.transcriptFile.Close()
		}
	}()
	// Byte-streamed tail. bufio.Scanner withholds anything that lacks a
	// newline; `claude -p` and similar tools may emit partial lines (progress
	// indicators, spinner frames, the final unterminated chunk). We flush at
	// each \n / \r, AND flush a pending partial line whenever a Read returns
	// no further data within the read window.
	buf := make([]byte, 4096)
	var pending []byte
	flush := func(rawLine string) {
		rawLine = stripANSI(rawLine)
		if rawLine == "" {
			return
		}
		// stream-json events are line-delimited JSON; pass through the parser
		// to get role-prefixed display lines + accumulated assistant text.
		var lines []string
		if rs.parser != nil {
			lines = rs.parser.Feed(rawLine)
		} else {
			lines = []string{rawLine}
		}
		for _, line := range lines {
			if line == "" {
				continue
			}
			if rs.transcriptFile != nil {
				fmt.Fprintln(rs.transcriptFile, line)
			}
			if m.emit != nil {
				m.emit(rs.sess.ID, line)
			}
			m.matchPatterns(rs, line)
		}
	}
	for {
		select {
		case <-ctx.Done():
			return
		default:
		}
		n, err := rs.pty.Read(buf)
		if n > 0 {
			for i := 0; i < n; i++ {
				c := buf[i]
				if c == '\n' || c == '\r' {
					if len(pending) > 0 {
						flush(string(pending))
						pending = pending[:0]
					}
				} else {
					pending = append(pending, c)
				}
			}
		}
		if err != nil {
			if len(pending) > 0 {
				flush(string(pending))
				pending = pending[:0]
			}
			break
		}
	}
	prev := rs.sess.State
	if err := rs.cmd.Wait(); err != nil {
		rs.sess.State = types.StateFailed
	} else if rs.sess.State == types.StateRunning {
		rs.sess.State = types.StateCompleted
	}
	end := time.Now()
	rs.sess.EndedAt = &end
	if m.store != nil {
		_ = m.store.UpdateSessionState(rs.sess.ID, rs.sess.State)
	}
	if m.onStateChange != nil && prev != rs.sess.State {
		m.onStateChange(*rs.sess, prev)
	}
	if m.bus != nil {
		m.bus.Publish(eventbus.EvtWorkerSlotFreed, rs.sess.ID)
	}
}

func (m *Manager) matchPatterns(rs *runtimeSession, line string) {
	prev := rs.sess.State
	switch {
	case strings.Contains(line, PatternQuestion):
		rs.sess.State = types.StateWaitingForInput
		rs.sess.LastPrompt = line
	case strings.Contains(line, PatternPlanWritten):
		// Extract the relative path the worker emitted. The sentinel ends with
		// ".prismconductor/plans/" so we pick up everything from that prefix
		// to the end of the line — yields ".prismconductor/plans/<num>-rev<N>.json".
		idx := strings.Index(line, ".prismconductor/plans/")
		path := strings.TrimSpace(line[idx:])
		if m.onPlanReady != nil {
			m.onPlanReady(*rs.sess, path)
		}
		// onPlanReady is responsible for publishing EvtPlanReady itself once it
		// has the plan id; if no handler is wired, publish a generic event.
		if m.onPlanReady == nil && m.bus != nil {
			m.bus.Publish(eventbus.EvtPlanReady, rs.sess.ID)
		}
	case strings.Contains(line, PatternComplete):
		rs.sess.State = types.StateCompleted
	case strings.Contains(line, PatternBlocked):
		rs.sess.State = types.StateBlocked
		if m.bus != nil {
			m.bus.Publish(eventbus.EvtWorkerBlocked, rs.sess.ID)
		}
	}
	if rs.sess.State != prev {
		if m.store != nil {
			_ = m.store.UpdateSessionState(rs.sess.ID, rs.sess.State)
		}
		if m.onStateChange != nil {
			m.onStateChange(*rs.sess, prev)
		}
	}
}

// Kill terminates a running session.
func (m *Manager) Kill(sessionID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	rs, ok := m.sessions[sessionID]
	if !ok {
		return fmt.Errorf("unknown session %s", sessionID)
	}
	rs.cancel()
	if rs.cmd.Process != nil {
		return rs.cmd.Process.Kill()
	}
	return nil
}

// SendInput writes user input into a session's PTY.
func (m *Manager) SendInput(sessionID, text string) error {
	m.mu.RLock()
	rs, ok := m.sessions[sessionID]
	m.mu.RUnlock()
	if !ok {
		return fmt.Errorf("unknown session %s", sessionID)
	}
	_, err := io.WriteString(rs.pty, text)
	return err
}

// Snapshot returns a copy of the current in-process session table.
func (m *Manager) Snapshot() []types.Session {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]types.Session, 0, len(m.sessions))
	for _, rs := range m.sessions {
		out = append(out, *rs.sess)
	}
	return out
}

// --- §10.4 dispatch ---
//
// `claude` parses positional args as the prompt; with -p (print mode) it runs
// non-interactively, emits clean line-buffered output, and exits when the
// prompt completes. That's exactly what plan- and execute-mode workers want
// per §10.1 / §10.2 — they're one-shot operations.

// claudeArgs are the universal flags every conductor-spawned worker uses:
//   - -p: non-interactive print mode (one-shot worker)
//   - --output-format stream-json + --include-partial-messages + --verbose:
//     emits a JSON event per token / tool call so the UI shows live progress
//     instead of waiting silently for the final response.
//   - --permission-mode acceptEdits: the worker writes plan / answer / code
//     files in the workspace's repo without an interactive approval gate
//     (which `-p` mode can't answer).
func claudeArgs(prompt string) []string {
	return []string{
		"claude",
		"-p",
		"--output-format", "stream-json",
		"--include-partial-messages",
		"--verbose",
		"--permission-mode", "acceptEdits",
		prompt,
	}
}

func buildPlanCommand(ws types.Workspace, issue types.Issue) []string {
	return claudeArgs(planPrompt(ws, issue))
}

func buildExecuteCommand(ws types.Workspace, issue types.Issue, plan types.Plan) []string {
	return claudeArgs(executePrompt(ws, issue, plan))
}

func planPrompt(ws types.Workspace, issue types.Issue) string {
	switch ws.SkillProfile.Mode {
	case types.SkillModeNative:
		return fmt.Sprintf("%s %d --emit-plan-json", ws.SkillProfile.NativePlanCommand, issue.Number)
	case types.SkillModeHybrid:
		return fmt.Sprintf("/conductor-plan --native-cmd %s --issue %d", ws.SkillProfile.NativePlanCommand, issue.Number)
	default:
		return fmt.Sprintf("/conductor-plan --issue %d --repo %s", issue.Number, ws.RepoPath)
	}
}

func executePrompt(ws types.Workspace, issue types.Issue, plan types.Plan) string {
	switch ws.SkillProfile.Mode {
	case types.SkillModeNative:
		return fmt.Sprintf("%s %d --resume-from-approved-plan %d", ws.SkillProfile.NativeExecuteCommand, issue.Number, plan.Revision)
	case types.SkillModeHybrid:
		return fmt.Sprintf("/conductor-execute --native-cmd %s --issue %d --revision %d", ws.SkillProfile.NativeExecuteCommand, issue.Number, plan.Revision)
	default:
		return fmt.Sprintf("/conductor-execute --issue %d --repo %s --revision %d", issue.Number, ws.RepoPath, plan.Revision)
	}
}

func envSpecToSlice(env types.EnvSpec) []string {
	out := make([]string, 0, len(env.EnvVars))
	for k, v := range env.EnvVars {
		out = append(out, k+"="+v)
	}
	return out
}
