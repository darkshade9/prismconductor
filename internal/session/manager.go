package session

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"

	"prismconductor/internal/eventbus"
	pcgit "prismconductor/internal/git"
	"prismconductor/internal/llm"
	"prismconductor/internal/types"
)

// LineHandler receives each PTY output line as it arrives.
type LineHandler func(sessionID string, line string)

// Persister is the slice of *store.Store that the session manager needs.
// Defined as an interface so the package doesn't depend on store directly.
type Persister interface {
	SaveSession(sess *types.Session, transcriptPath string) error
	UpdateSessionState(id string, state types.SessionState) error
	UpdateSessionPendingQuestion(id, questionID string) error
	UpdateSessionTranscriptOffset(id string, off int64) error
}

// StateChangeHandler is fired on every state transition. Used by the App layer
// to fan out OS notifications + Wails events.
type StateChangeHandler func(sess types.Session, prev types.SessionState)

// PlanReadyHandler is fired when the worker prints the §10.3 "Plan written"
// sentinel line. The handler is responsible for reading the file off disk and
// persisting it.
type PlanReadyHandler func(sess types.Session, planPath string)

// PROpenedHandler is fired when the worker prints the §10.3 "PR_OPENED: <url>"
// sentinel. The handler is responsible for parsing the PR number, persisting
// it on the issue row, and publishing EvtPROpened.
type PROpenedHandler func(sess types.Session, prURL string)

// ActivityHandler receives a session-liveness ping (most recent tool call,
// running tool count). Used to drive the UI's "still alive, doing X" hint.
// Implementations should debounce — emissions are already throttled at the
// manager level but each session may fire concurrently.
type ActivityHandler func(types.SessionActivity)

type Manager struct {
	bus           *eventbus.Bus
	emit          LineHandler
	transcriptDir string
	store         Persister
	onStateChange StateChangeHandler
	onPlanReady   PlanReadyHandler
	onPROpened    PROpenedHandler
	onActivity    ActivityHandler
	providers     *llm.Registry

	mu       sync.RWMutex
	sessions map[string]*runtimeSession
}

type runtimeSession struct {
	sess           *types.Session
	cmd            *exec.Cmd
	cancel         context.CancelFunc
	transcriptPath string
	transcriptFile *os.File
	parser         *StreamParser

	// Issue #54: byte offset into transcriptPath of the last fully-flushed line.
	// Persisted every ~2s during live tail; on restart we seek here to avoid
	// re-feeding lines we already processed.
	transcriptOffset int64

	actMu         sync.Mutex
	toolCount     int
	lastAction    string
	lastActionAt  time.Time
	lastEmittedAt time.Time

	// Issue #22: per-execute worktree fields. Empty for plan/raw spawns.
	// repoPath is captured here so the cleanup hook in tailAndParse doesn't
	// need a workspace registry lookup at teardown time.
	worktreeDir string
	branch      string
	repoPath    string

	// Issue #27: pool the slot was reserved against. Threaded through to the
	// EvtWorkerSlotFreed payload so the orchestrator releases the right pool.
	poolID string

	// Issue #54: true when this runtimeSession is a re-attach (no owned
	// process). Reattach paths skip cmd.Wait + worktree teardown that the
	// fresh-spawn path needs.
	reattached bool
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

// SetOnPROpened registers the handler invoked when a worker prints the
// "PR_OPENED: <url>" sentinel.
func (m *Manager) SetOnPROpened(h PROpenedHandler) { m.onPROpened = h }

// SetOnActivity registers a handler called on tool-call activity (throttled
// to ~2/sec/session). Used to drive the UI's per-card liveness indicator.
func (m *Manager) SetOnActivity(h ActivityHandler) { m.onActivity = h }

// SetProviders wires the LLM provider registry. Required before SpawnPlan /
// SpawnExecute since those resolve the per-pool argv via prov.SpawnArgs.
func (m *Manager) SetProviders(r *llm.Registry) { m.providers = r }

// SpawnPlan launches a plan-mode worker per §10.1 / §10.4. The pool's provider
// determines the argv via Provider.SpawnArgs — Claude pools today, additional
// providers when harness-v1 lands.
func (m *Manager) SpawnPlan(ws types.Workspace, issue types.Issue, pool types.Pool) (*types.Session, error) {
	args, err := m.buildPlanCommand(ws, issue, pool)
	if err != nil {
		return nil, err
	}
	return m.spawn(ws, issue, types.ModePlan, args, pool.ID)
}

// SpawnExecute launches an execute-mode worker per §10.2.
//
// Issue #22: each execute runs inside a per-(workspace, issue) git worktree
// off origin/<DefaultBranch>. The conductor — not the skill — owns the
// worktree's lifecycle: created here before pty.Start, torn down by
// tailAndParse on Blocked/Failed (immediate, q2=A) or by the 24h GC walk on
// Completed (q3=A).
func (m *Manager) SpawnExecute(ws types.Workspace, issue types.Issue, plan types.Plan, pool types.Pool) (*types.Session, error) {
	base := ws.DefaultBranch
	if base == "" {
		base = "main"
	}
	slug := branchSlug(issue.Title)
	branch := fmt.Sprintf("feat/issue-%d-%s", issue.Number, slug)
	worktreeDir := filepath.Join(ws.RepoPath, ".prismconductor", "worktrees",
		fmt.Sprintf("%s-%d", ws.ID, issue.Number))

	// Idempotency: a prior failed run may have left a worktree at this exact
	// path. Force-remove first so `worktree add -B` succeeds. The error is
	// ignored because the common case is "no such worktree" — Add will fail
	// loudly if a real problem remains.
	_ = pcgit.Remove(ws.RepoPath, worktreeDir)
	if err := pcgit.Add(ws.RepoPath, branch, worktreeDir, base); err != nil {
		return nil, fmt.Errorf("prepare worktree: %w", err)
	}

	// q4=D: auto-init submodules inside the new worktree so the worker has a
	// complete checkout. The cost is paid up-front (potentially minutes for
	// large submodules) instead of risking a mid-run BLOCKED on a missing path.
	if pcgit.HasSubmodules(worktreeDir) {
		if err := pcgit.InitSubmodules(worktreeDir); err != nil {
			_ = pcgit.Remove(ws.RepoPath, worktreeDir)
			return nil, fmt.Errorf("prepare worktree (submodules): %w", err)
		}
	}

	// q1=A: copy plan + answers JSON from the main checkout's .prismconductor/
	// (gitignored, so absent in the fresh worktree) so the worker's cwd-
	// relative reads succeed. Hard fail on missing plan — a worker can't
	// execute without it.
	if err := mirrorPlanArtifacts(ws.RepoPath, worktreeDir, issue.Number, plan.Revision); err != nil {
		_ = pcgit.Remove(ws.RepoPath, worktreeDir)
		return nil, fmt.Errorf("mirror plan artifacts: %w", err)
	}

	args, err := m.buildExecuteCommand(ws, issue, plan, pool)
	if err != nil {
		_ = pcgit.Remove(ws.RepoPath, worktreeDir)
		return nil, err
	}
	sess, err := m.spawnWithDir(ws, issue, types.ModeExecute, args, worktreeDir, branch, pool.ID)
	if err != nil {
		_ = pcgit.Remove(ws.RepoPath, worktreeDir)
		return nil, err
	}
	return sess, nil
}

// SpawnExecuteResume re-enters an execute worker on an existing worktree to
// continue work after a mid-run question (#17, Q5/Q6). The branch and
// worktree are expected to already exist on disk — this is the second leg of
// a paused-for-question flow, NOT a fresh execute. Returns an error if the
// worktree is missing so the caller can surface it on the OLD session row.
func (m *Manager) SpawnExecuteResume(ws types.Workspace, issue types.Issue, plan types.Plan, pool types.Pool, questionID string) (*types.Session, error) {
	slug := branchSlug(issue.Title)
	branch := fmt.Sprintf("feat/issue-%d-%s", issue.Number, slug)
	worktreeDir := filepath.Join(ws.RepoPath, ".prismconductor", "worktrees",
		fmt.Sprintf("%s-%d", ws.ID, issue.Number))
	if _, err := os.Stat(worktreeDir); err != nil {
		return nil, fmt.Errorf("resume worktree missing at %s: %w", worktreeDir, err)
	}
	args, err := m.buildExecuteResumeCommand(ws, issue, plan, pool, questionID)
	if err != nil {
		return nil, err
	}
	return m.spawnWithDir(ws, issue, types.ModeExecute, args, worktreeDir, branch, pool.ID)
}

// branchSlug derives a kebab-case branch suffix from an issue title, lowering
// case, replacing non-alphanumeric runs with single dashes, capping at 40
// characters, and falling back to "work" when the title yields nothing.
func branchSlug(title string) string {
	title = strings.ToLower(title)
	var b strings.Builder
	prevDash := true
	for _, r := range title {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			b.WriteRune(r)
			prevDash = false
		default:
			if !prevDash {
				b.WriteByte('-')
				prevDash = true
			}
		}
		if b.Len() >= 40 {
			break
		}
	}
	out := strings.Trim(b.String(), "-")
	if out == "" {
		out = "work"
	}
	return out
}

// mirrorPlanArtifacts copies the rev-N plan (required) and answers (optional,
// pre-execute may have no questions) JSON files from the main checkout's
// .prismconductor/ into the worktree's .prismconductor/. The worker's cwd is
// the worktree, so its relative reads in the conductor-execute skill resolve
// here.
func mirrorPlanArtifacts(repoPath, worktreeDir string, num, rev int) error {
	pairs := []struct {
		subdir   string
		name     string
		required bool
	}{
		{"plans", fmt.Sprintf("%d-rev%d.json", num, rev), true},
		{"answers", fmt.Sprintf("%d-rev%d.json", num, rev), false},
	}
	for _, p := range pairs {
		src := filepath.Join(repoPath, ".prismconductor", p.subdir, p.name)
		info, err := os.Stat(src)
		if err != nil {
			if p.required {
				return fmt.Errorf("plan file missing: %s", src)
			}
			continue
		}
		if info.IsDir() {
			continue
		}
		dst := filepath.Join(worktreeDir, ".prismconductor", p.subdir, p.name)
		if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
			return err
		}
		b, err := os.ReadFile(src)
		if err != nil {
			return err
		}
		if err := os.WriteFile(dst, b, 0o644); err != nil {
			return err
		}
	}
	return nil
}

// SpawnRaw runs a non-skill command via PTY (used by the day-1 demo: `claude --version`).
func (m *Manager) SpawnRaw(ws types.Workspace, name string, args []string) (*types.Session, error) {
	demoIssue := types.Issue{Number: 0, WorkspaceID: ws.ID}
	full := append([]string{name}, args...)
	return m.spawn(ws, demoIssue, types.ModePlan, full, "")
}

func (m *Manager) spawn(ws types.Workspace, issue types.Issue, mode types.SessionMode, argv []string, poolID string) (*types.Session, error) {
	return m.spawnWithDir(ws, issue, mode, argv, "", "", poolID)
}

// spawnWithDir is the canonical spawn path. When worktreeDir is non-empty the
// child process runs there instead of ws.RepoPath, and the worktree metadata
// is captured on the runtimeSession for the cleanup hook in tailAndParse.
//
// Issue #54: the worker's stdout/stderr point at the transcript file directly
// so it survives a conductor exit (no Go-side copy goroutine that would die
// with us and break the pipe). The conductor reads worker output by polling
// the same file (tailAndParse). This also unifies the live-spawn path with
// the re-attach path: both consume the file.
func (m *Manager) spawnWithDir(ws types.Workspace, issue types.Issue, mode types.SessionMode, argv []string, worktreeDir, branch, poolID string) (*types.Session, error) {
	if len(argv) == 0 {
		return nil, fmt.Errorf("empty command")
	}
	if m.transcriptDir == "" {
		// The transcript file is now load-bearing — it IS the worker's stdout.
		// Without a transcript dir we have nowhere to point cmd.Stdout, so refuse.
		return nil, fmt.Errorf("session manager: transcript dir not configured")
	}
	if err := os.MkdirAll(m.transcriptDir, 0o755); err != nil {
		return nil, fmt.Errorf("create transcript dir: %w", err)
	}
	sessID := uuid.NewString()
	transcriptPath := filepath.Join(m.transcriptDir, sessID+".log")
	tf, err := os.Create(transcriptPath)
	if err != nil {
		return nil, fmt.Errorf("create transcript: %w", err)
	}

	cmd := exec.Command(argv[0], argv[1:]...)
	switch {
	case worktreeDir != "":
		cmd.Dir = worktreeDir
	case ws.RepoPath != "":
		cmd.Dir = ws.RepoPath
	}
	cmd.Env = append(os.Environ(), envSpecToSlice(ws.AgentEnv)...)
	cmd.Stdin = nil
	cmd.Stdout = tf
	cmd.Stderr = tf
	cmd.SysProcAttr = detachedProcAttr()

	if err := cmd.Start(); err != nil {
		_ = tf.Close()
		_ = os.Remove(transcriptPath)
		return nil, fmt.Errorf("cmd.Start: %w", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	sess := &types.Session{
		ID:          sessID,
		WorkspaceID: ws.ID,
		IssueNumber: issue.Number,
		Mode:        mode,
		State:       types.StateRunning,
		StartedAt:   time.Now(),
		PID:         cmd.Process.Pid,
	}
	sess.Transcript = transcriptPath

	rs := &runtimeSession{
		sess:           sess,
		cmd:            cmd,
		cancel:         cancel,
		parser:         NewStreamParser(),
		transcriptPath: transcriptPath,
		transcriptFile: tf,
		worktreeDir:    worktreeDir,
		branch:         branch,
		repoPath:       ws.RepoPath,
		poolID:         poolID,
	}

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

// tailAndParse polls the transcript file for new lines, feeds each through
// the per-session parser, and routes the resulting display lines to emit /
// matchPatterns / recordActivity. Issue #54 replaces the old PTY byte-stream
// reader with a file tail because the worker's stdout IS the transcript file
// now (so the conductor exiting doesn't break the worker's writes).
func (m *Manager) tailAndParse(ctx context.Context, rs *runtimeSession) {
	defer func() {
		// Conductor's handle on the transcript file. Closing it here doesn't
		// affect the worker — the worker has its own fd via fork+exec dup.
		if rs.transcriptFile != nil {
			_ = rs.transcriptFile.Close()
		}
	}()

	// Run Wait in a sibling goroutine so the tail loop can detect "child has
	// exited" without blocking on Wait itself. Buffered so the goroutine can
	// always return even if we never read from done.
	done := make(chan error, 1)
	go func() { done <- rs.cmd.Wait() }()

	var waitErr error
	var waited bool
	if rf, err := os.Open(rs.transcriptPath); err != nil {
		log.Printf("tailAndParse: open transcript %s: %v", rs.transcriptPath, err)
	} else {
		reader := bufio.NewReader(rf)
		var lastFlush time.Time
	tail:
		for {
			select {
			case <-ctx.Done():
				break tail
			default:
			}
			line, rerr := reader.ReadString('\n')
			if len(line) > 0 {
				m.feedLine(rs, strings.TrimRight(line, "\r\n"))
				rs.transcriptOffset += int64(len(line))
				if m.store != nil && time.Since(lastFlush) >= 2*time.Second {
					_ = m.store.UpdateSessionTranscriptOffset(rs.sess.ID, rs.transcriptOffset)
					lastFlush = time.Now()
				}
			}
			if rerr == io.EOF {
				select {
				case werr := <-done:
					// Worker exited. Drain anything still buffered + anything
					// the worker wrote between our last read and its exit.
					waitErr = werr
					waited = true
					for {
						line, rerr := reader.ReadString('\n')
						if len(line) > 0 {
							m.feedLine(rs, strings.TrimRight(line, "\r\n"))
							rs.transcriptOffset += int64(len(line))
						}
						if rerr != nil {
							break
						}
					}
					break tail
				default:
					time.Sleep(200 * time.Millisecond)
					continue
				}
			}
			if rerr != nil {
				log.Printf("tailAndParse: read %s: %v", rs.transcriptPath, rerr)
				break tail
			}
		}
		_ = rf.Close()
	}

	// Persist the final offset so a subsequent restart skips what we already
	// processed.
	if m.store != nil {
		_ = m.store.UpdateSessionTranscriptOffset(rs.sess.ID, rs.transcriptOffset)
	}
	prev := rs.sess.State
	if !waited {
		// We broke out without observing Wait — typically because ctx was
		// cancelled. Wait now to reap the child and capture its exit code.
		waitErr = <-done
	}
	rs.sess.State = mapTerminalState(rs.sess.State, waitErr)
	end := time.Now()
	rs.sess.EndedAt = &end
	if m.store != nil {
		_ = m.store.UpdateSessionState(rs.sess.ID, rs.sess.State)
	}
	if m.onStateChange != nil && prev != rs.sess.State {
		m.onStateChange(*rs.sess, prev)
	}
	if m.bus != nil {
		m.bus.Publish(eventbus.EvtWorkerSlotFreed, eventbus.WorkerSlotFreed{
			SessionID: rs.sess.ID,
			PoolID:    rs.poolID,
		})
	}

	// Issue #22: per-execute worktree teardown.
	//   q2=A: Blocked/Failed → immediate `git worktree remove --force`. The
	//         user loses post-mortem `cd` access to the worktree but gains
	//         deterministic state with no goroutine fanout. A leftover from a
	//         transient failure is reclaimed by the next startup Prune.
	//   q3=A: Completed → leave on disk; the 24h GC walk reclaims it later.
	// Issue #17: PausedForQuestion must also keep the worktree on disk so the
	// resume worker can `git switch` back into it. The 24h GC reclaims if the
	// user never answers.
	if rs.worktreeDir != "" {
		switch rs.sess.State {
		case types.StateBlocked, types.StateFailed:
			if err := pcgit.Remove(rs.repoPath, rs.worktreeDir); err != nil {
				log.Printf("worktree cleanup (terminal=%s) %s: %v",
					rs.sess.State, rs.worktreeDir, err)
			}
		}
	}

	// Drop from in-process registry so Card iteration doesn't keep finding
	// it. The transcript + DB row remain.
	m.mu.Lock()
	delete(m.sessions, rs.sess.ID)
	m.mu.Unlock()
}

// feedLine routes one raw line (post-newline-strip, pre-ANSI-strip) through
// the session's parser and downstream handlers. Used both by the live
// tailAndParse goroutine and the post-restart re-attach paths.
//
// Issue #54: this method does NOT write to the transcript file. The worker's
// stdout is already the transcript file, so anything we'd emit here is a
// duplicate. The conductor only consumes.
func (m *Manager) feedLine(rs *runtimeSession, raw string) {
	raw = stripANSI(raw)
	if raw == "" {
		return
	}
	var lines []string
	if rs.parser != nil {
		lines = rs.parser.Feed(raw)
	} else {
		lines = []string{raw}
	}
	for _, line := range lines {
		if line == "" {
			continue
		}
		if m.emit != nil {
			m.emit(rs.sess.ID, line)
		}
		m.matchPatterns(rs, line)
		m.recordActivity(rs, line)
	}
}

// mapTerminalState collapses an in-flight session state to its terminal value
// once the worker process exits. Pure function so it's unit-testable without a
// real PTY. Issue #17 added the StatePausedForQuestion arm: a paused session
// that exits cleanly stays paused (the QUESTION_PENDING sentinel already
// committed the pause), and a paused session that exits non-zero is also kept
// paused — the worker's exit code doesn't change the fact that the user owes
// an answer.
func mapTerminalState(prev types.SessionState, waitErr error) types.SessionState {
	if prev == types.StatePausedForQuestion {
		return types.StatePausedForQuestion
	}
	if waitErr != nil {
		return types.StateFailed
	}
	switch prev {
	case types.StateRunning:
		return types.StateCompleted
	case types.StateBlocked, types.StateWaitingForInput:
		return types.StateFailed
	}
	return prev
}

func (m *Manager) matchPatterns(rs *runtimeSession, line string) {
	prev := rs.sess.State
	switch {
	case strings.Contains(line, PatternQuestionPending):
		// Mid-run question (#17). Capture the trailing UUID; flip the session
		// to paused_for_question so the answer-file watcher can pick it up.
		// Order matters: this arm must run BEFORE PatternQuestion (which only
		// fires for legacy plan-mode "Question: " lines). The two prefixes
		// don't collide on case-sensitive contains, but listing this case
		// first is the safer ordering if either ever changes.
		idx := strings.Index(line, PatternQuestionPending)
		qid := strings.TrimSpace(line[idx+len(PatternQuestionPending):])
		if qid == "" {
			log.Printf("QUESTION_PENDING sentinel with empty id on session %s", rs.sess.ID)
			break
		}
		// One-in-flight: ignore a second QUESTION_PENDING while a question is
		// already pending. Concurrent mid-run questions on the same issue are
		// out of scope per the issue body's Non-goals.
		if rs.sess.PendingQuestionID != "" {
			log.Printf("session %s: ignoring second QUESTION_PENDING %s (already pending %s)",
				rs.sess.ID, qid, rs.sess.PendingQuestionID)
			break
		}
		rs.sess.State = types.StatePausedForQuestion
		rs.sess.PendingQuestionID = qid
		// Persist both. UpdateSessionState only writes one column, so we save
		// the full row to capture the new id and the JSON blob.
		if m.store != nil {
			_ = m.store.SaveSession(rs.sess, rs.transcriptPath)
		}
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
	case strings.Contains(line, PatternPROpened):
		// No state mutation here — worker keeps running until PatternComplete.
		// Mirrors the PatternPlanWritten arm shape.
		idx := strings.Index(line, PatternPROpened)
		url := strings.TrimSpace(line[idx+len(PatternPROpened):])
		if url == "" {
			log.Printf("PR_OPENED sentinel with empty URL on session %s", rs.sess.ID)
		} else if m.onPROpened != nil {
			m.onPROpened(*rs.sess, url)
		}
	case strings.Contains(line, PatternComplete):
		rs.sess.State = types.StateCompleted
	case strings.Contains(line, PatternBlocked):
		rs.sess.State = types.StateBlocked
		// Capture the reason text after the BLOCKED: prefix so the UI can
		// surface it on the card. The line is already stripped + role-prefixed
		// (e.g. "@asst BLOCKED: tests failed — 3 failures") so we hunt for
		// the literal sentinel and take the rest of the line.
		if i := strings.Index(line, PatternBlocked); i >= 0 {
			reason := strings.TrimSpace(line[i+len(PatternBlocked):])
			if len(reason) > 500 {
				reason = reason[:500]
			}
			rs.sess.BlockedReason = reason
			// Re-save the full JSON so BlockedReason survives restart.
			// UpdateSessionState only updates the `state` column.
			if m.store != nil {
				_ = m.store.SaveSession(rs.sess, rs.transcriptPath)
			}
		}
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

// recordActivity ticks the per-session counters when a tool call goes by and
// emits a throttled activity event. RoleTool ("@tool ") prefix is the canonical
// signal from streamjson.go.
func (m *Manager) recordActivity(rs *runtimeSession, line string) {
	if !strings.HasPrefix(line, RoleTool) {
		return
	}
	action := strings.TrimSpace(line[len(RoleTool):])
	// Trim the JSON args off the action label — keep just the tool name +
	// optional first arg word for the card display.
	if i := strings.Index(action, " "); i > 0 {
		action = action[:i]
	}
	now := time.Now()
	rs.actMu.Lock()
	rs.toolCount++
	rs.lastAction = action
	rs.lastActionAt = now
	emit := false
	if now.Sub(rs.lastEmittedAt) >= 500*time.Millisecond {
		rs.lastEmittedAt = now
		emit = true
	}
	count := rs.toolCount
	rs.actMu.Unlock()

	if emit && m.onActivity != nil {
		m.onActivity(types.SessionActivity{
			SessionID:    rs.sess.ID,
			WorkspaceID:  rs.sess.WorkspaceID,
			IssueNumber:  rs.sess.IssueNumber,
			ToolCount:    count,
			LastAction:   action,
			LastActionAt: now,
		})
	}
}

// Kill terminates a running session. For re-attached sessions whose original
// process is owned by a prior conductor (issue #54), we still send a kill
// signal directly since the worker is in our PID namespace.
func (m *Manager) Kill(sessionID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	rs, ok := m.sessions[sessionID]
	if !ok {
		return fmt.Errorf("unknown session %s", sessionID)
	}
	if rs.cancel != nil {
		rs.cancel()
	}
	if rs.cmd != nil && rs.cmd.Process != nil {
		return rs.cmd.Process.Kill()
	}
	if rs.sess != nil && rs.sess.PID > 0 {
		if p, err := os.FindProcess(rs.sess.PID); err == nil {
			return p.Kill()
		}
	}
	return nil
}

// SendInput is a no-op since #54. The spawn path no longer allocates a PTY,
// so there's no input channel to write to. Skill workers run with
// --bypassPermissions and never need post-spawn input; if a future caller
// needs it, restore a controlled stdin pipe here.
func (m *Manager) SendInput(sessionID, text string) error {
	m.mu.RLock()
	_, ok := m.sessions[sessionID]
	m.mu.RUnlock()
	if !ok {
		return fmt.Errorf("unknown session %s", sessionID)
	}
	return fmt.Errorf("session input not supported (detached worker, no stdin)")
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
// Worker argv is provided by the LLM provider registry: each pool's Provider
// returns the argv via SpawnArgs(pool, prompt). Today only the Claude provider
// returns a working argv; the others return llm.ErrNotSupported until
// harness-v1 lands. The prompt itself is mode-specific (plan / execute) and
// shaped here.

func (m *Manager) buildPlanCommand(ws types.Workspace, issue types.Issue, pool types.Pool) ([]string, error) {
	return m.providerArgs(pool, planPrompt(ws, issue))
}

func (m *Manager) buildExecuteCommand(ws types.Workspace, issue types.Issue, plan types.Plan, pool types.Pool) ([]string, error) {
	return m.providerArgs(pool, executePrompt(ws, issue, plan))
}

// buildExecuteResumeCommand mirrors buildExecuteCommand but appends the
// `--resume-question <id>` flag so the conductor-execute skill knows to skip
// branch creation and read the mid-run question's context sidecar (#17).
func (m *Manager) buildExecuteResumeCommand(ws types.Workspace, issue types.Issue, plan types.Plan, pool types.Pool, questionID string) ([]string, error) {
	return m.providerArgs(pool, executeResumePrompt(ws, issue, plan, questionID))
}

func (m *Manager) providerArgs(pool types.Pool, prompt string) ([]string, error) {
	if m.providers == nil {
		return nil, fmt.Errorf("session manager: provider registry not configured")
	}
	prov, ok := m.providers.Get(pool.Provider)
	if !ok {
		return nil, fmt.Errorf("session manager: unknown provider %q for pool %s", pool.Provider, pool.ID)
	}
	return prov.SpawnArgs(pool, prompt)
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

// executeResumePrompt appends the `--resume-question <id>` flag so the
// conductor-execute skill knows to skip branch creation and read the mid-run
// question's context sidecar (#17). Hybrid + Native variants get the same
// suffix; the native command is expected to forward the flag through to its
// underlying execute skill.
func executeResumePrompt(ws types.Workspace, issue types.Issue, plan types.Plan, questionID string) string {
	return executePrompt(ws, issue, plan) + " --resume-question " + questionID
}

func envSpecToSlice(env types.EnvSpec) []string {
	out := make([]string, 0, len(env.EnvVars))
	for k, v := range env.EnvVars {
		out = append(out, k+"="+v)
	}
	return out
}
