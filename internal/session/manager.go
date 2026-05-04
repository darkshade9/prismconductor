package session

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
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
	"prismconductor/internal/harness"
	"prismconductor/internal/llm"
	"prismconductor/internal/remoteworker"
	"prismconductor/internal/skills/bundle"
	"prismconductor/internal/types"
)

// loadSkillMarkdown reads the skill markdown for the given stage from the
// workspace's PerStage configuration. Returns empty string (not an error) when
// no per-stage skill is configured — the harness falls through to its legacy
// mode logic.
func loadSkillMarkdown(ws types.Workspace, stage types.ConductorStage) string {
	ref, ok := ws.SkillProfile.SkillForStage(stage)
	if !ok {
		return ""
	}
	switch ref.Source {
	case "bundled":
		name := strings.TrimPrefix(ref.Path, "bundled:")
		if b, err := fs.ReadFile(bundle.FS, "skills/"+name+".md"); err == nil {
			return string(b)
		}
	case "repo":
		if b, err := os.ReadFile(ref.Path); err == nil {
			return string(b)
		}
	}
	return ""
}

// LineHandler receives each PTY output line as it arrives.
type LineHandler func(sessionID string, line string)

// Persister is the slice of *store.Store that the session manager needs.
// Defined as an interface so the package doesn't depend on store directly.
type Persister interface {
	SaveSession(sess *types.Session, transcriptPath string) error
	UpdateSessionState(id string, state types.SessionState) error
	UpdateSessionPendingQuestion(id, questionID string) error
	UpdateSessionTranscriptOffset(id string, off int64) error
	// AccumulateIssueWork adds elapsed seconds to the issue's work timer (issue #46).
	AccumulateIssueWork(workspaceID string, number int, mode types.SessionMode, seconds int64) error
	// AccumulateIssueCost adds LLM cost to the issue's cost_usd counter (issue #47).
	AccumulateIssueCost(workspaceID string, number int, costUSD float64) error
	// UpdateSessionUsage persists per-session token counts and estimated cost
	// to the dedicated columns and JSON blob (issue #101).
	UpdateSessionUsage(sess *types.Session, transcriptPath string) error
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

// NeedsPRHandler is fired when the worker prints the "NEEDS_PR: <reason>"
// sentinel (#157). The handler is responsible for persisting NeedsPRInfo on
// the issue row, moving the card to REVIEW, and publishing EvtNeedsPR.
type NeedsPRHandler func(sess types.Session, branch, worktreeDir, reason string)

// ActivityHandler receives a session-liveness ping (most recent tool call,
// running tool count). Used to drive the UI's "still alive, doing X" hint.
// Implementations should debounce — emissions are already throttled at the
// manager level but each session may fire concurrently.
type ActivityHandler func(types.SessionActivity)

// RateLimitHandler is called whenever a Claude stream emits a rate_limit_event.
// Implementations should be non-blocking.
type RateLimitHandler func([]types.PoolUsage)

type Manager struct {
	bus            *eventbus.Bus
	emit           LineHandler
	transcriptDir  string
	store          Persister
	onStateChange  StateChangeHandler
	onPlanReady    PlanReadyHandler
	onPROpened     PROpenedHandler
	onNeedsPR      NeedsPRHandler
	onActivity     ActivityHandler
	onRateLimit    RateLimitHandler
	providers      *llm.Registry
	poolReg        PoolRehydrator

	mu       sync.RWMutex
	sessions map[string]*runtimeSession

	// inFlight tracks (workspace, issue, mode) tuples whose spawn is currently
	// mid-setup but not yet registered in `sessions`. Protects against a TOCTOU
	// race where two concurrent callers both pre-check, both see no existing
	// session, and both proceed to spawn — doubling LLM cost on the same issue.
	// Key shape: "<workspaceID>:<issueNumber>:<mode>". Held under `mu` writes.
	inFlight map[string]bool
}

// ErrDuplicateSpawn is returned by SpawnPlan / SpawnExecute when a worker
// for the same (workspace, issue, mode) is already running, waiting, blocked,
// or mid-setup. Caller MUST release any pool slot it acquired before calling.
// Sentinel so callers can branch on it cleanly via errors.Is.
var ErrDuplicateSpawn = errors.New("duplicate spawn refused: same (workspace, issue, mode) already in flight")

type runtimeSession struct {
	sess           *types.Session
	cmd            sessionCmd
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
	actRing       activityRing

	// Issue #22: per-execute worktree fields. Empty for plan/raw spawns.
	// repoPath is captured here so the cleanup hook in tailAndParse doesn't
	// need a workspace registry lookup at teardown time.
	worktreeDir string
	branch      string
	repoPath    string

	// Issue #27: pool the slot was reserved against. Threaded through to the
	// EvtWorkerSlotFreed payload so the orchestrator releases the right pool.
	poolID   string
	poolName string

	// Issue #54: true when this runtimeSession is a re-attach (no owned
	// process). Reattach paths skip cmd.Wait + worktree teardown that the
	// fresh-spawn path needs.
	reattached bool

	// Issue #58: harness strategy stash. The harness goroutine writes its
	// terminal error here so synthCmd.Wait can return it, letting
	// mapTerminalState distinguish a clean exit from a transport failure or
	// context cancel without requiring the harness to bubble through PTY
	// stdout.
	harnessErr error

	// Issue #101: pool model name captured at spawn time so ResolveUsage can
	// look up per-token rates when computing estimated cost for harness sessions.
	poolModel string
	// harnessInputTokens / harnessOutputTokens accumulate token counts from
	// the harness OnTurnUsage callback. Zero for subprocess (Claude) sessions
	// where CostData from the stream parser is authoritative.
	harnessInputTokens  int64
	harnessOutputTokens int64

	// slotReleased is set to true the first time EvtWorkerSlotFreed is
	// published for this session. Guards against double-release when both the
	// normal tailAndParse cleanup path and the safety-net defer race to free
	// the pool slot. Protected by actMu.
	slotReleased bool

	// lastLineAt is the time of the most recent transcript line consumed by
	// tailAndParse. Initialised to the session start time so new sessions
	// aren't immediately considered stale by the watchdog. Protected by actMu.
	lastLineAt time.Time

	// autoKilledForPROpen is set by KillAfterPROpened before the kill signal
	// is sent. tailAndParse uses it to override StateFailed → StateCompleted
	// so the worktree is preserved on the normal Completed cleanup path.
	// Protected by actMu.
	autoKilledForPROpen bool
}

// sessionCmd is the §10.5 abstraction over both spawn strategies. The
// subprocess strategy wraps an *exec.Cmd; the harness strategy wraps the
// goroutine that drives the in-process agent loop. Both expose Wait + Kill
// + Pid so tailAndParse, Kill, and the persistence layer don't care which
// strategy a session uses.
type sessionCmd interface {
	Wait() error
	Kill() error
	Pid() int
}

// subprocCmd adapts an *exec.Cmd (Claude pools, raw demo spawns) to the
// sessionCmd interface.
type subprocCmd struct {
	cmd *exec.Cmd
}

func (s *subprocCmd) Wait() error {
	return s.cmd.Wait()
}

func (s *subprocCmd) Kill() error {
	if s.cmd == nil || s.cmd.Process == nil {
		return nil
	}
	return s.cmd.Process.Kill()
}

func (s *subprocCmd) Pid() int {
	if s.cmd == nil || s.cmd.Process == nil {
		return 0
	}
	return s.cmd.Process.Pid
}

// synthCmd adapts the harness goroutine to the sessionCmd interface. Wait
// blocks until the harness signals done, then returns the captured terminal
// error (which mapTerminalState turns into StateFailed unless matchPatterns
// already saw a sentinel that drove the state somewhere else).
type synthCmd struct {
	done   chan struct{}
	err    *error
	cancel context.CancelFunc
}

func (s *synthCmd) Wait() error {
	<-s.done
	if s.err == nil {
		return nil
	}
	return *s.err
}

func (s *synthCmd) Kill() error {
	if s.cancel != nil {
		s.cancel()
	}
	return nil
}

func (s *synthCmd) Pid() int { return 0 }

func NewManager(bus *eventbus.Bus, emit LineHandler) *Manager {
	return &Manager{
		bus:      bus,
		emit:     emit,
		sessions: map[string]*runtimeSession{},
		inFlight: map[string]bool{},
	}
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

// SetOnNeedsPR registers the handler invoked when a worker prints the
// "NEEDS_PR: <reason>" sentinel (#157).
func (m *Manager) SetOnNeedsPR(h NeedsPRHandler) { m.onNeedsPR = h }

// SetOnActivity registers a handler called on tool-call activity (throttled
// to ~2/sec/session). Used to drive the UI's per-card liveness indicator.
func (m *Manager) SetOnActivity(h ActivityHandler) { m.onActivity = h }

// SetOnRateLimit registers a handler called when a Claude session emits a
// rate_limit_event in its stream JSON.
func (m *Manager) SetOnRateLimit(h RateLimitHandler) { m.onRateLimit = h }

// SetProviders wires the LLM provider registry. Required before SpawnPlan /
// SpawnExecute since those resolve the per-pool argv via prov.SpawnArgs.
func (m *Manager) SetProviders(r *llm.Registry) { m.providers = r }

// SpawnPlan launches a plan-mode worker per §10.1 / §10.4. The pool's provider
// determines the spawn strategy: Claude returns argv (subprocess), the four
// OpenAI-compat providers return llm.ErrNotSupported (harness, §10.5).
func (m *Manager) SpawnPlan(ws types.Workspace, issue types.Issue, pool types.Pool) (*types.Session, error) {
	args, prompt, err := m.buildPlanCommand(ws, issue, pool)
	if err != nil {
		return nil, err
	}
	skillMarkdown := loadSkillMarkdown(ws, types.StagePlan)
	return m.spawn(ws, issue, types.ModePlan, args, prompt, pool, skillMarkdown)
}

// SpawnExecute launches an execute-mode worker per §10.2.
//
// Issue #22: each execute runs inside a per-(workspace, issue) git worktree
// off origin/<DefaultBranch>. The conductor — not the skill — owns the
// worktree's lifecycle: created here before pty.Start, torn down by
// tailAndParse on Blocked/Failed (immediate, q2=A) or by the 24h GC walk on
// Completed (q3=A).
//
// Issue #171: when ws.ExecutionTarget is "remote", delegates to spawnRemote
// instead of creating a local worktree. Local flow is unchanged.
func (m *Manager) SpawnExecute(ws types.Workspace, issue types.Issue, plan types.Plan, pool types.Pool) (*types.Session, error) {
	if ws.ExecutionTarget == types.ExecutionTargetRemote && ws.RemoteConfig != nil {
		return m.spawnRemote(ws, issue, plan, pool, "")
	}
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

	args, prompt, err := m.buildExecuteCommand(ws, issue, plan, pool)
	if err != nil {
		_ = pcgit.Remove(ws.RepoPath, worktreeDir)
		return nil, err
	}
	skillMarkdown := loadSkillMarkdown(ws, types.StageExecute)
	sess, err := m.spawnWithDir(ws, issue, types.ModeExecute, args, prompt, worktreeDir, branch, pool, skillMarkdown)
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
//
// Issue #171: remote workspaces resume via spawnRemote with the questionID
// forwarded; the remote worker handles re-hydrating the branch.
func (m *Manager) SpawnExecuteResume(ws types.Workspace, issue types.Issue, plan types.Plan, pool types.Pool, questionID string) (*types.Session, error) {
	if ws.ExecutionTarget == types.ExecutionTargetRemote && ws.RemoteConfig != nil {
		return m.spawnRemote(ws, issue, plan, pool, questionID)
	}
	slug := branchSlug(issue.Title)
	branch := fmt.Sprintf("feat/issue-%d-%s", issue.Number, slug)
	worktreeDir := filepath.Join(ws.RepoPath, ".prismconductor", "worktrees",
		fmt.Sprintf("%s-%d", ws.ID, issue.Number))
	if _, err := os.Stat(worktreeDir); err != nil {
		return nil, fmt.Errorf("resume worktree missing at %s: %w", worktreeDir, err)
	}
	args, prompt, err := m.buildExecuteResumeCommand(ws, issue, plan, pool, questionID)
	if err != nil {
		return nil, err
	}
	skillMarkdown := loadSkillMarkdown(ws, types.StageContinue)
	return m.spawnWithDir(ws, issue, types.ModeExecute, args, prompt, worktreeDir, branch, pool, skillMarkdown)
}

// SpawnExecuteContinue re-enters an execute worker on the existing feature
// branch to continue work after review feedback or test failures. The worktree
// is reused if it exists on disk; if missing it is rehydrated from
// origin/<branch> (q2=A). The user's note must already be written to
// .prismconductor/notes/<num>.txt in the main checkout before calling this.
func (m *Manager) SpawnExecuteContinue(ws types.Workspace, issue types.Issue, plan types.Plan, pool types.Pool) (*types.Session, error) {
	slug := branchSlug(issue.Title)
	branch := fmt.Sprintf("feat/issue-%d-%s", issue.Number, slug)
	worktreeDir := filepath.Join(ws.RepoPath, ".prismconductor", "worktrees",
		fmt.Sprintf("%s-%d", ws.ID, issue.Number))

	if _, err := os.Stat(worktreeDir); err != nil {
		// Worktree is missing — rehydrate from origin/<branch>.
		if err2 := pcgit.Add(ws.RepoPath, branch, worktreeDir, branch); err2 != nil {
			return nil, fmt.Errorf("rehydrate worktree for continue: %w", err2)
		}
	}

	if err := mirrorPlanArtifacts(ws.RepoPath, worktreeDir, issue.Number, plan.Revision); err != nil {
		return nil, fmt.Errorf("mirror plan artifacts: %w", err)
	}
	if err := mirrorContinueNote(ws.RepoPath, worktreeDir, issue.Number); err != nil {
		return nil, fmt.Errorf("mirror continue note: %w", err)
	}

	args, prompt, err := m.buildExecuteContinueCommand(ws, issue, plan, pool)
	if err != nil {
		return nil, err
	}
	skillMarkdown := loadSkillMarkdown(ws, types.StageContinue)
	return m.spawnWithDir(ws, issue, types.ModeExecute, args, prompt, worktreeDir, branch, pool, skillMarkdown)
}

// mirrorContinueNote copies .prismconductor/notes/<num>.txt from the main
// checkout into the worktree so the conductor-continue skill can read it.
func mirrorContinueNote(repoPath, worktreeDir string, num int) error {
	name := fmt.Sprintf("%d.txt", num)
	src := filepath.Join(repoPath, ".prismconductor", "notes", name)
	if _, err := os.Stat(src); os.IsNotExist(err) {
		return nil // Note missing server-side; skill will BLOCKED with a clear message.
	}
	dst := filepath.Join(worktreeDir, ".prismconductor", "notes", name)
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	b, err := os.ReadFile(src)
	if err != nil {
		return err
	}
	return os.WriteFile(dst, b, 0o644)
}

func (m *Manager) buildExecuteContinueCommand(ws types.Workspace, issue types.Issue, plan types.Plan, pool types.Pool) ([]string, string, error) {
	return m.providerArgs(pool, executeContinuePrompt(ws, issue, plan))
}

func executeContinuePrompt(ws types.Workspace, issue types.Issue, plan types.Plan) string {
	if ref, ok := ws.SkillProfile.SkillForStage(types.StageContinue); ok {
		if ref.Source == "bundled" {
			return fmt.Sprintf("/%s --issue %d --repo %s --revision %d", ref.Name, issue.Number, ws.RepoPath, plan.Revision)
		}
		return fmt.Sprintf("Continue work on issue #%d (revision %d) in repo %s", issue.Number, plan.Revision, ws.RepoPath)
	}
	// Legacy fallback.
	switch ws.SkillProfile.Mode {
	case types.SkillModeNative:
		return fmt.Sprintf("%s %d --continue-revision %d", ws.SkillProfile.NativeExecuteCommand, issue.Number, plan.Revision)
	case types.SkillModeHybrid:
		return fmt.Sprintf("/conductor-continue --native-cmd %s --issue %d --revision %d", ws.SkillProfile.NativeExecuteCommand, issue.Number, plan.Revision)
	default:
		return fmt.Sprintf("/conductor-continue --issue %d --repo %s --revision %d", issue.Number, ws.RepoPath, plan.Revision)
	}
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

// SpawnRaw runs a non-skill command via subprocess (used by the day-1 demo:
// `claude --version`). Always takes the subprocess strategy — there's no pool
// to dispatch on.
func (m *Manager) SpawnRaw(ws types.Workspace, name string, args []string) (*types.Session, error) {
	demoIssue := types.Issue{Number: 0, WorkspaceID: ws.ID}
	full := append([]string{name}, args...)
	return m.spawn(ws, demoIssue, types.ModePlan, full, "", types.Pool{}, "")
}

func (m *Manager) spawn(ws types.Workspace, issue types.Issue, mode types.SessionMode, argv []string, prompt string, pool types.Pool, skillMarkdown string) (*types.Session, error) {
	return m.spawnWithDir(ws, issue, mode, argv, prompt, "", "", pool, skillMarkdown)
}

// spawnWithDir is the canonical spawn path. When worktreeDir is non-empty the
// worker runs there instead of ws.RepoPath, and the worktree metadata is
// captured on the runtimeSession for the cleanup hook in tailAndParse.
//
// Issue #54: the worker's stdout/stderr point at the transcript file directly
// so it survives a conductor exit (no Go-side copy goroutine that would die
// with us and break the pipe). The conductor reads worker output by polling
// the same file (tailAndParse). This also unifies the live-spawn path with
// the re-attach path: both consume the file.
//
// Issue #58: dispatches between two strategies — subprocess (Claude pools,
// raw demo spawns) and harness (the four OpenAI-compat providers). When argv
// is set, the subprocess path runs as before. When argv is empty (the
// caller's Provider.SpawnArgs returned llm.ErrNotSupported) the harness
// goroutine drives the agent loop and writes synthesized stream-json into
// the transcript file. tailAndParse + StreamParser see identical input
// shape regardless of strategy.
func (m *Manager) spawnWithDir(ws types.Workspace, issue types.Issue, mode types.SessionMode, argv []string, prompt, worktreeDir, branch string, pool types.Pool, skillMarkdown string) (*types.Session, error) {
	if len(argv) == 0 && prompt == "" {
		return nil, fmt.Errorf("empty command")
	}

	// Atomic duplicate-spawn guard. Two concurrent callers — e.g. a manual
	// drag-to-PLAN racing against the orchestrator's auto-pull drain, or two
	// near-simultaneous bus events — used to both pre-check via
	// hasActivePlanSession (reads m.Snapshot()), both see no existing session
	// (because neither had registered yet), and both proceed to spawn. Two
	// plan workers for the same issue → DOUBLE LLM cost. Witnessed live on
	// issue #109 (two state=running plan rows, both pid=0 harness).
	//
	// Prevention: take the lock before any I/O, check both `inFlight` (a
	// spawn is mid-setup but not yet registered in `sessions`) AND the
	// `sessions` map for any non-terminal same-(workspace, issue, mode)
	// session. Reserve the in-flight key under the same lock so a sibling
	// goroutine arriving any time after this point bails.
	//
	// SpawnRaw uses Number=0 demo issues; skip the guard for those (multiple
	// `claude --version` style probes are fine).
	if issue.Number > 0 {
		key := fmt.Sprintf("%s:%d:%s", ws.ID, issue.Number, mode)
		m.mu.Lock()
		if m.inFlight[key] {
			m.mu.Unlock()
			return nil, ErrDuplicateSpawn
		}
		for _, rs := range m.sessions {
			if rs == nil || rs.sess == nil {
				continue
			}
			if rs.sess.WorkspaceID != ws.ID || rs.sess.IssueNumber != issue.Number || rs.sess.Mode != mode {
				continue
			}
			switch rs.sess.State {
			case types.StateRunning, types.StateWaitingForInput, types.StatePausedForQuestion, types.StateBlocked:
				m.mu.Unlock()
				return nil, ErrDuplicateSpawn
			}
		}
		m.inFlight[key] = true
		m.mu.Unlock()

		// Clear the reservation on every exit path. The successful path also
		// clears it after registering the session in m.sessions; doing it here
		// in defer is the belt-and-suspenders for early-error returns below
		// (transcript create fails, harness build errors, etc.).
		defer func() {
			m.mu.Lock()
			delete(m.inFlight, key)
			m.mu.Unlock()
		}()
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

	ctx, cancel := context.WithCancel(context.Background())
	sess := &types.Session{
		ID:          sessID,
		WorkspaceID: ws.ID,
		IssueNumber: issue.Number,
		Mode:        mode,
		State:       types.StateRunning,
		StartedAt:   time.Now(),
		PoolID:      pool.ID,
	}
	sess.Transcript = transcriptPath

	rs := &runtimeSession{
		sess:           sess,
		cancel:         cancel,
		parser:         NewStreamParser(),
		transcriptPath: transcriptPath,
		transcriptFile: tf,
		worktreeDir:    worktreeDir,
		branch:         branch,
		repoPath:       ws.RepoPath,
		poolID:         pool.ID,
		poolName:       pool.Name,
		poolModel:      pool.Model,
		lastLineAt:     time.Now(),
	}

	if len(argv) > 0 {
		// Subprocess strategy.
		cwd := worktreeDir
		if cwd == "" {
			cwd = ws.RepoPath
		}
		cmd := exec.Command(argv[0], argv[1:]...)
		cmd.Dir = cwd
		cmd.Env = append(os.Environ(), envSpecToSlice(ws.AgentEnv)...)
		cmd.Stdin = nil
		cmd.Stdout = tf
		cmd.Stderr = tf
		cmd.SysProcAttr = detachedProcAttr()
		if err := cmd.Start(); err != nil {
			_ = tf.Close()
			_ = os.Remove(transcriptPath)
			cancel()
			return nil, fmt.Errorf("cmd.Start: %w", err)
		}
		sess.PID = cmd.Process.Pid
		rs.cmd = &subprocCmd{cmd: cmd}
	} else {
		// Harness strategy (issue #58). The harness goroutine writes
		// synthesized stream-json directly into the transcript file; the
		// conductor's tail loop reads it as it would for a Claude PTY.
		if m.providers == nil {
			_ = tf.Close()
			_ = os.Remove(transcriptPath)
			cancel()
			return nil, fmt.Errorf("session manager: provider registry not configured")
		}
		prov, ok := m.providers.Get(pool.Provider)
		if !ok {
			_ = tf.Close()
			_ = os.Remove(transcriptPath)
			cancel()
			return nil, fmt.Errorf("session manager: unknown provider %q for pool %s", pool.Provider, pool.ID)
		}
		done := m.spawnHarness(ctx, rs, prov, ws, pool, prompt, skillMarkdown)
		rs.cmd = &synthCmd{done: done, err: &rs.harnessErr, cancel: cancel}
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

// spawnRemote creates a session that runs inside a remote Cloudflare Worker
// (issue #171). It mirrors the local spawnWithDir session setup — same
// duplicate-spawn guard, same transcript file, same tailAndParse goroutine —
// but instead of starting a subprocess it connects to the remote worker's
// /sessions endpoint and streams SSE transcript lines into the local file.
//
// questionID is non-empty only on resume-after-question legs.
func (m *Manager) spawnRemote(ws types.Workspace, issue types.Issue, plan types.Plan, pool types.Pool, questionID string) (*types.Session, error) {
	rc := ws.RemoteConfig
	if rc == nil || rc.CFWorkerEndpointURL == "" {
		return nil, fmt.Errorf("remote workspace %s has no worker endpoint configured", ws.ID)
	}

	// Duplicate-spawn guard (same logic as spawnWithDir).
	key := fmt.Sprintf("%s:%d:%s", ws.ID, issue.Number, types.ModeExecute)
	m.mu.Lock()
	if m.inFlight[key] {
		m.mu.Unlock()
		return nil, ErrDuplicateSpawn
	}
	for _, rs := range m.sessions {
		if rs == nil || rs.sess == nil {
			continue
		}
		if rs.sess.WorkspaceID != ws.ID || rs.sess.IssueNumber != issue.Number || rs.sess.Mode != types.ModeExecute {
			continue
		}
		switch rs.sess.State {
		case types.StateRunning, types.StateWaitingForInput, types.StatePausedForQuestion, types.StateBlocked:
			m.mu.Unlock()
			return nil, ErrDuplicateSpawn
		}
	}
	m.inFlight[key] = true
	m.mu.Unlock()
	defer func() {
		m.mu.Lock()
		delete(m.inFlight, key)
		m.mu.Unlock()
	}()

	if m.transcriptDir == "" {
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

	ctx, cancel := context.WithCancel(context.Background())
	sess := &types.Session{
		ID:          sessID,
		WorkspaceID: ws.ID,
		IssueNumber: issue.Number,
		Mode:        types.ModeExecute,
		State:       types.StateRunning,
		StartedAt:   time.Now(),
		PoolID:      pool.ID,
	}
	sess.Transcript = transcriptPath

	slug := branchSlug(issue.Title)
	branch := fmt.Sprintf("feat/issue-%d-%s", issue.Number, slug)

	params := remoteworker.SpawnParams{
		WorkspaceID:   ws.ID,
		IssueNumber:   issue.Number,
		Mode:          string(types.ModeExecute),
		GitHubOwner:   ws.GitHubOwner,
		GitHubRepo:    ws.GitHubRepo,
		DefaultBranch: ws.DefaultBranch,
		PlanRevision:  plan.Revision,
		QuestionID:    questionID,
		PoolModel:     pool.Model,
	}
	remCmd, err := remoteworker.Spawn(ctx, rc.CFWorkerEndpointURL, params, tf)
	if err != nil {
		_ = tf.Close()
		_ = os.Remove(transcriptPath)
		cancel()
		return nil, fmt.Errorf("remote spawn: %w", err)
	}

	rs := &runtimeSession{
		sess:           sess,
		cmd:            remCmd,
		cancel:         cancel,
		parser:         NewStreamParser(),
		transcriptPath: transcriptPath,
		transcriptFile: tf,
		branch:         branch,
		poolID:         pool.ID,
		poolName:       pool.Name,
		poolModel:      pool.Model,
		lastLineAt:     time.Now(),
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

// releaseSlot publishes EvtWorkerSlotFreed for rs exactly once (idempotent via
// slotReleased). Safe to call from both the normal tailAndParse cleanup path
// and the safety-net defer — whichever fires first wins and the other is a
// no-op.
func (m *Manager) releaseSlot(rs *runtimeSession) {
	rs.actMu.Lock()
	released := rs.slotReleased
	rs.slotReleased = true
	rs.actMu.Unlock()
	if released {
		return
	}
	if m.bus != nil {
		m.bus.Publish(eventbus.EvtWorkerSlotFreed, eventbus.WorkerSlotFreed{
			SessionID: rs.sess.ID,
			PoolID:    rs.poolID,
		})
	}
}

// tailAndParse polls the transcript file for new lines, feeds each through
// the per-session parser, and routes the resulting display lines to emit /
// matchPatterns / recordActivity. Issue #54 replaces the old PTY byte-stream
// reader with a file tail because the worker's stdout IS the transcript file
// now (so the conductor exiting doesn't break the worker's writes).
func (m *Manager) tailAndParse(ctx context.Context, rs *runtimeSession) {
	// Safety-net (issue #152): registered first so it fires last in LIFO order,
	// after the transcript-close defer below. Guarantees that regardless of how
	// this goroutine exits — including a panic in the function body — the pool
	// slot is released and the session is removed from m.sessions. On the normal
	// (no-panic) path every call here is a no-op: the function body already ran
	// the same cleanup, and releaseSlot is idempotent via slotReleased.
	defer func() {
		if r := recover(); r != nil {
			log.Printf("tailAndParse panic: session %s: %v", rs.sess.ID, r)
			end := time.Now()
			if rs.sess.EndedAt == nil {
				rs.sess.EndedAt = &end
			}
			if rs.sess.State == types.StateRunning || rs.sess.State == types.StateWaitingForInput {
				rs.sess.State = types.StateFailed
			}
			if m.store != nil {
				_ = m.store.UpdateSessionState(rs.sess.ID, rs.sess.State)
			}
		}
		m.releaseSlot(rs)
		m.mu.Lock()
		delete(m.sessions, rs.sess.ID)
		m.mu.Unlock()
	}()

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
				rs.actMu.Lock()
				rs.lastLineAt = time.Now()
				rs.actMu.Unlock()
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
							rs.actMu.Lock()
							rs.lastLineAt = time.Now()
							rs.actMu.Unlock()
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
	rs.actMu.Lock()
	autoKilled := rs.autoKilledForPROpen
	rs.actMu.Unlock()
	if autoKilled && rs.sess.State == types.StateFailed {
		// Worker was killed by KillAfterPROpened after emitting PR_OPENED.
		// Treat as Completed so the worktree is preserved on the normal path.
		log.Printf("auto-cancel: PR opened; execute session %s for issue #%d completed after %.0fs",
			rs.sess.ID, rs.sess.IssueNumber, time.Since(rs.sess.StartedAt).Seconds())
		rs.sess.State = types.StateCompleted
	}
	end := time.Now()
	rs.sess.EndedAt = &end
	if m.store != nil {
		_ = m.store.UpdateSessionState(rs.sess.ID, rs.sess.State)
		// Accumulate work time for plan/execute sessions so the card timer persists
		// across restarts. We record the full session duration regardless of
		// terminal state — partial work (failed, paused) still counts (issue #46).
		if rs.sess.Mode == types.ModePlan || rs.sess.Mode == types.ModeExecute {
			secs := int64(end.Sub(rs.sess.StartedAt).Seconds())
			if secs > 0 {
				_ = m.store.AccumulateIssueWork(rs.sess.WorkspaceID, rs.sess.IssueNumber, rs.sess.Mode, secs)
			}
		}
		// Persist per-session token counts and estimated cost (issue #101).
		// Claude subprocess sessions get data from the stream-parser result
		// event; harness sessions from the OnTurnUsage accumulator.
		rs.actMu.Lock()
		hIn := rs.harnessInputTokens
		hOut := rs.harnessOutputTokens
		rs.actMu.Unlock()
		inputTok, outputTok, costCents := ResolveUsage(rs.parser.LastCost(), hIn, hOut, rs.poolModel)
		rs.sess.InputTokens = inputTok
		rs.sess.OutputTokens = outputTok
		rs.sess.EstimatedCostCents = costCents
		_ = m.store.UpdateSessionUsage(rs.sess, rs.transcriptPath)
		// Accumulate LLM cost from Claude stream-json result event (issue #47).
		// Only Claude subprocess sessions emit this; harness sessions don't.
		cd := rs.parser.LastCost()
		if cd != nil && cd.TotalCostUSD > 0 {
			_ = m.store.AccumulateIssueCost(rs.sess.WorkspaceID, rs.sess.IssueNumber, cd.TotalCostUSD)
		} else if costCents > 0 {
			// Harness sessions: accumulate estimated cost so the card chip shows spend.
			_ = m.store.AccumulateIssueCost(rs.sess.WorkspaceID, rs.sess.IssueNumber, costCents/100)
		}
	}
	if m.onStateChange != nil && prev != rs.sess.State {
		m.onStateChange(*rs.sess, prev)
	}
	m.releaseSlot(rs)

	// Worktree teardown policy.
	//
	// HISTORICAL (issue #22 q2=A): Blocked/Failed → immediate
	// `git worktree remove --force`. That was a DATA-LOSS BUG: when a
	// worker finished its edits but failed at `git commit -S` (GPG
	// passphrase timeout, signing key missing, etc.), the diff lived
	// only in the worktree's working directory and the cleanup nuked
	// it. The user paid for the LLM work and got nothing.
	//
	// NEW POLICY: NEVER auto-remove a worktree that contains
	// user-paid-for work. Specifically:
	//   - Completed → keep on disk (24h GC walk reclaims later;
	//     unchanged from before).
	//   - PausedForQuestion → keep on disk so the resume worker can
	//     `git switch` back into it (issue #17; unchanged).
	//   - Blocked / Failed → keep on disk if there's ANY uncommitted
	//     work, untracked files, or commits ahead of the base branch.
	//     Only safe-remove when the worktree is verifiably empty.
	//
	// "Verifiably empty" means `git status --porcelain` returns no
	// lines AND no commits exist ahead of the base. If git isn't even
	// reachable (worktree corrupted), default to KEEP — better to leak
	// a dir than wipe data we can't inspect.
	//
	// The 24h GC walk handles long-term cleanup of legitimately empty
	// worktrees that were never resumed.
	if rs.worktreeDir != "" {
		switch rs.sess.State {
		case types.StateBlocked, types.StateFailed:
			if pcgit.WorktreeIsEmpty(rs.worktreeDir) {
				if err := pcgit.Remove(rs.repoPath, rs.worktreeDir); err != nil {
					log.Printf("worktree cleanup (terminal=%s, empty) %s: %v",
						rs.sess.State, rs.worktreeDir, err)
				}
			} else {
				log.Printf("worktree preserved (terminal=%s) %s: contains uncommitted work; user can recover via `cd %s`",
					rs.sess.State, rs.worktreeDir, rs.worktreeDir)
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
	// Opportunistically parse Claude rate_limit_event system events before
	// stripping role-prefix tags. The raw JSON is only present on Claude
	// (subprocess) sessions; for harness sessions this is always a no-op.
	if m.onRateLimit != nil && rs.poolID != "" {
		if usages, ok := llm.ParseClaudeRateLimitEvent(raw, rs.poolID, rs.poolName); ok {
			m.onRateLimit(usages)
		}
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
	if prev == types.StateNeedsPR {
		return types.StateNeedsPR
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

// sentinelAtLineStart reports whether the worker's text on this line begins
// with `pat`. Lines from the StreamParser are role-prefixed (e.g.
// `@asst BLOCKED: tests failed`), so the sentinel must follow the prefix
// not be embedded mid-sentence. Plain string-only check per CLAUDE.md;
// just anchored to the start so quoting/discussion of the literal sentinel
// text inside an assistant message can't false-positive a state transition.
func sentinelAtLineStart(line, pat string) bool {
	// Strip the 6-char role prefix `@xxxx ` if present. RoleAssistant /
	// RoleSystem / RoleResult / RoleUser / RoleTool / RoleLive are all six
	// characters wide ending in a space, so a fixed-width skip works without
	// importing the role table.
	if len(line) >= 6 && line[0] == '@' && line[5] == ' ' {
		line = line[6:]
	}
	return strings.HasPrefix(line, pat)
}

func (m *Manager) matchPatterns(rs *runtimeSession, line string) {
	prev := rs.sess.State
	// Sentinel matching is anchored to the start of the assistant text (after
	// the role prefix). Without anchoring, any plan/execute run that quotes
	// or discusses the literal text — e.g. a ticket body that contains the
	// word "BLOCKED:" — flips the session into the wrong terminal state on
	// the quoting line, even though the worker is happily mid-task. The
	// skill contract already requires sentinels to be the leading text of
	// their own line, so anchoring is just enforcing what the worker should
	// be doing anyway. Per CLAUDE.md "PTY pattern matching is plain string
	// contains, not regex" — this stays string-only, just at the line head.
	switch {
	case sentinelAtLineStart(line, PatternQuestionPending):
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
	case sentinelAtLineStart(line, PatternQuestion):
		rs.sess.State = types.StateWaitingForInput
		rs.sess.LastPrompt = line
	case sentinelAtLineStart(line, PatternPlanWritten):
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
	case sentinelAtLineStart(line, PatternPROpened):
		// No state mutation here — worker keeps running until PatternComplete.
		// Mirrors the PatternPlanWritten arm shape.
		idx := strings.Index(line, PatternPROpened)
		url := strings.TrimSpace(line[idx+len(PatternPROpened):])
		if url == "" {
			log.Printf("PR_OPENED sentinel with empty URL on session %s", rs.sess.ID)
		} else if m.onPROpened != nil {
			m.onPROpened(*rs.sess, url)
		}
	case sentinelAtLineStart(line, PatternComplete):
		rs.sess.State = types.StateCompleted
	case sentinelAtLineStart(line, PatternNeedsPR):
		// NEEDS_PR (#157): execute completed the work but push/signing failed.
		// Transition to the terminal StateNeedsPR state and fire the handler so
		// app.go can move the card to REVIEW with a NeedsPRInfo badge.
		rs.sess.State = types.StateNeedsPR
		idx := strings.Index(line, PatternNeedsPR)
		reason := strings.TrimSpace(line[idx+len(PatternNeedsPR):])
		if len(reason) > 500 {
			reason = reason[:500]
		}
		rs.sess.BlockedReason = reason
		if m.store != nil {
			_ = m.store.SaveSession(rs.sess, rs.transcriptPath)
		}
		if m.onNeedsPR != nil {
			m.onNeedsPR(*rs.sess, rs.branch, rs.worktreeDir, reason)
		}
	case sentinelAtLineStart(line, PatternBlocked):
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

// recordActivity ticks the per-session counters when a tool call goes by,
// pushes an ActivityEntry to the in-memory ring buffer (issue #99), and
// emits a throttled activity event. RoleTool ("@tool ") prefix is the
// canonical signal from streamjson.go.
func (m *Manager) recordActivity(rs *runtimeSession, line string) {
	if !strings.HasPrefix(line, RoleTool) {
		return
	}
	entry := newActivityEntry(line)
	action := entry.ToolName
	now := entry.At
	rs.actMu.Lock()
	rs.toolCount++
	rs.lastAction = action
	rs.lastActionAt = now
	rs.actRing.push(entry)
	emit := false
	if now.Sub(rs.lastEmittedAt) >= 500*time.Millisecond {
		rs.lastEmittedAt = now
		emit = true
	}
	count := rs.toolCount
	tail := rs.actRing.tail()
	rs.actMu.Unlock()

	if emit && m.onActivity != nil {
		m.onActivity(types.SessionActivity{
			SessionID:           rs.sess.ID,
			WorkspaceID:         rs.sess.WorkspaceID,
			IssueNumber:         rs.sess.IssueNumber,
			ToolCount:           count,
			LastAction:          action,
			LastActionAt:        now,
			LastToolArgsSummary: entry.ArgsSummary,
			Tail:                tail,
		})
	}
}

// Kill terminates a running session. For re-attached sessions whose original
// process is owned by a prior conductor (issue #54), we still send a kill
// signal directly since the worker is in our PID namespace. For harness
// sessions (issue #58), Kill cancels the harness context — the goroutine
// unwinds, closes the transcript handle, and surfaces context.Canceled
// through synthCmd.Wait so mapTerminalState lands on Failed.
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
	if rs.cmd != nil {
		if err := rs.cmd.Kill(); err != nil {
			return err
		}
		return nil
	}
	if rs.sess != nil && rs.sess.PID > 0 {
		if p, err := os.FindProcess(rs.sess.PID); err == nil {
			return p.Kill()
		}
	}
	return nil
}

// KillGraceful sends SIGTERM to a running subprocess session and force-kills
// it after a 3-second grace period (issue #99, q3=A). For harness sessions
// (no subprocess) the context cancel is sufficient. Safe to call concurrently.
func (m *Manager) KillGraceful(sessionID string) error {
	m.mu.Lock()
	rs, ok := m.sessions[sessionID]
	m.mu.Unlock()
	if !ok {
		return fmt.Errorf("unknown session %s", sessionID)
	}
	if rs.cancel != nil {
		rs.cancel()
	}
	sub, ok := rs.cmd.(*subprocCmd)
	if !ok || sub.cmd == nil || sub.cmd.Process == nil {
		return nil
	}
	proc := sub.cmd.Process
	if err := proc.Signal(gracefulSignal()); err != nil {
		// Already dead or permission denied — escalate to SIGKILL immediately.
		return proc.Kill()
	}
	go func() {
		time.Sleep(3 * time.Second)
		_ = proc.Kill()
	}()
	return nil
}

// KillAfterPROpened finds the running or waiting execute session for
// (workspaceID, issueNumber), marks it as auto-killed so tailAndParse will
// record it as Completed (preserving the worktree), kills the process, and
// returns the session ID, start time, and a best-effort estimated cost in
// cents. Returns found=false when no active execute session exists (safe to
// call concurrently). Issue #118.
func (m *Manager) KillAfterPROpened(workspaceID string, issueNumber int) (id string, startedAt time.Time, estimatedCostCents float64, found bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, rs := range m.sessions {
		s := rs.sess
		if s.WorkspaceID != workspaceID || s.IssueNumber != issueNumber || s.Mode != types.ModeExecute {
			continue
		}
		if s.State != types.StateRunning && s.State != types.StateWaitingForInput {
			continue
		}
		rs.actMu.Lock()
		rs.autoKilledForPROpen = true
		hIn := rs.harnessInputTokens
		hOut := rs.harnessOutputTokens
		rs.actMu.Unlock()
		_, _, costCents := ResolveUsage(rs.parser.LastCost(), hIn, hOut, rs.poolModel)
		if rs.cancel != nil {
			rs.cancel()
		}
		if rs.cmd != nil {
			_ = rs.cmd.Kill()
		}
		return s.ID, s.StartedAt, costCents, true
	}
	return "", time.Time{}, 0, false
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

// FindActiveExecuteSession returns the session ID and start time of any active
// execute-mode session for the given workspace and issue (#113). Active means
// the session state is running, waiting_for_input, or paused_for_question.
// Returns found=false when no such session exists (safe to call concurrently).
func (m *Manager) FindActiveExecuteSession(workspaceID string, issueNumber int) (id string, startedAt time.Time, found bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	for _, rs := range m.sessions {
		s := rs.sess
		if s.WorkspaceID != workspaceID || s.IssueNumber != issueNumber || s.Mode != types.ModeExecute {
			continue
		}
		switch s.State {
		case types.StateRunning, types.StateWaitingForInput, types.StatePausedForQuestion:
			return s.ID, s.StartedAt, true
		}
	}
	return "", time.Time{}, false
}

// --- §10.4 / §10.5 dispatch ---
//
// Worker argv is provided by the LLM provider registry: each pool's Provider
// returns the argv via SpawnArgs(pool, prompt). Claude returns argv (the
// session manager runs it as a subprocess); the four OpenAI-compat providers
// return llm.ErrNotSupported (the session manager runs the in-process
// harness loop). The prompt itself is mode-specific (plan / execute) and
// shaped here. Both strategies share the same prompt — the harness uses it
// as the user-message in its first chat turn.

func (m *Manager) buildPlanCommand(ws types.Workspace, issue types.Issue, pool types.Pool) ([]string, string, error) {
	return m.providerArgs(pool, planPrompt(ws, issue))
}

func (m *Manager) buildExecuteCommand(ws types.Workspace, issue types.Issue, plan types.Plan, pool types.Pool) ([]string, string, error) {
	return m.providerArgs(pool, executePrompt(ws, issue, plan))
}

// buildExecuteResumeCommand mirrors buildExecuteCommand but appends the
// `--resume-question <id>` flag so the conductor-execute skill knows to skip
// branch creation and read the mid-run question's context sidecar (#17).
func (m *Manager) buildExecuteResumeCommand(ws types.Workspace, issue types.Issue, plan types.Plan, pool types.Pool, questionID string) ([]string, string, error) {
	return m.providerArgs(pool, executeResumePrompt(ws, issue, plan, questionID))
}

// providerArgs returns the spawn strategy for a (pool, prompt) pair:
//   - argv non-nil, err nil       → subprocess strategy
//   - argv nil, prompt non-empty  → harness strategy (provider returned
//                                   llm.ErrNotSupported from SpawnArgs, which
//                                   is the §10.5 signal to use the in-process
//                                   loop)
//   - other error                 → bubbled to caller
func (m *Manager) providerArgs(pool types.Pool, prompt string) ([]string, string, error) {
	if m.providers == nil {
		return nil, "", fmt.Errorf("session manager: provider registry not configured")
	}
	prov, ok := m.providers.Get(pool.Provider)
	if !ok {
		return nil, "", fmt.Errorf("session manager: unknown provider %q for pool %s", pool.Provider, pool.ID)
	}
	argv, err := prov.SpawnArgs(pool, prompt)
	if err == nil {
		return argv, prompt, nil
	}
	if errors.Is(err, llm.ErrNotSupported) {
		return nil, prompt, nil
	}
	return nil, "", err
}

func planPrompt(ws types.Workspace, issue types.Issue) string {
	if ref, ok := ws.SkillProfile.SkillForStage(types.StagePlan); ok {
		if ref.Source == "bundled" {
			return fmt.Sprintf("/%s --issue %d --repo %s", ref.Name, issue.Number, ws.RepoPath)
		}
		// Repo skill: metadata only; system prompt carries the skill markdown.
		return fmt.Sprintf("Plan issue #%d in repo %s", issue.Number, ws.RepoPath)
	}
	// Legacy fallback.
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
	if ref, ok := ws.SkillProfile.SkillForStage(types.StageExecute); ok {
		if ref.Source == "bundled" {
			return fmt.Sprintf("/%s --issue %d --repo %s --revision %d", ref.Name, issue.Number, ws.RepoPath, plan.Revision)
		}
		return fmt.Sprintf("Execute plan for issue #%d (revision %d) in repo %s", issue.Number, plan.Revision, ws.RepoPath)
	}
	// Legacy fallback.
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

// poolBudget builds the effective harness.Budget for a pool by overlaying
// any per-pool overrides on top of DefaultBudget() (issue #89). Nil fields
// fall back to the global default — existing pools with no overrides are
// unaffected.
func poolBudget(p types.Pool) harness.Budget {
	b := harness.DefaultBudget()
	if p.MaxTurns != nil {
		b.MaxTurns = *p.MaxTurns
	}
	if p.MaxInputTokens != nil {
		b.MaxInputTokens = *p.MaxInputTokens
	}
	if p.BashTimeout != nil {
		b.BashTimeout = *p.BashTimeout
	}
	if p.OutputCap != nil {
		b.OutputCap = *p.OutputCap
	}
	return b
}
