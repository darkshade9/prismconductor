package main

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"

	wruntime "github.com/wailsapp/wails/v2/pkg/runtime"

	"github.com/google/uuid"
	gh "github.com/google/go-github/v62/github"

	"prismconductor/internal/agentterm"
	"prismconductor/internal/archiver"
	"prismconductor/internal/diagnose"
	"prismconductor/internal/eventbus"
	pcgit "prismconductor/internal/git"
	pcgithub "prismconductor/internal/github"
	"prismconductor/internal/handlers"
	"prismconductor/internal/issueview"
	"prismconductor/internal/logbuffer"
	"prismconductor/internal/goalfilter"
	"prismconductor/internal/planio"
	"prismconductor/internal/githubauth"
	"prismconductor/internal/llm"
	"prismconductor/internal/orchestrator"
	"prismconductor/internal/remoteworker"
	"prismconductor/internal/secretstore"
	"prismconductor/internal/session"
	"prismconductor/internal/pipeline"
	"prismconductor/internal/skills"
	"prismconductor/internal/skills/bundle"
	"prismconductor/internal/store"
	"prismconductor/internal/types"
	"prismconductor/internal/workerpool"
	"prismconductor/internal/workspace"
)

// App is the Wails-bound root. Methods are exposed to the frontend.
type App struct {
	ctx context.Context

	bus       *eventbus.Bus
	store     *store.Store
	mgr       *session.Manager
	poolReg   *workerpool.Registry
	providers *llm.Registry
	orch      *orchestrator.Orchestrator
	wsReg     *workspace.Registry
	auth      *githubauth.Client
	gh        *pcgithub.Client
	poller    *pcgithub.Poller
	logs      *logbuffer.Ring
	cfgDir    string

	answerWatcher *store.AnswerWatcher
	assembler     *issueview.Assembler

	pendingDevice *githubauth.DeviceCode

	notifyMu      sync.Mutex
	lastNotifyKey string
	lastNotifyAt  int64

	issueDetailCache sync.Map // key: "wsID#number" → issueDetailEntry

	// Issue #116: per-PR self-heal attempt counters.
	// Key: "wsID#issueNum#headSHA", value: int.
	healAttempts sync.Map

	// Issue #161: ephemeral PTY-backed agent terminal sessions.
	agentTerm *agentterm.Manager

	// Issue #178: OS-native secret store for CF API tokens.
	secretStore secretstore.Store
}

type issueDetailEntry struct {
	issue     types.Issue
	expiresAt time.Time
}

func NewApp() *App { return &App{} }

func (a *App) startup(ctx context.Context) {
	a.ctx = ctx

	cfgDir, _ := configDir()
	a.cfgDir = cfgDir

	// Issue #178: OS-native secret store for CF API tokens.
	a.secretStore = secretstore.NewKeychainStore()

	// Capture every log.Printf into a ring buffer so the UI can show them.
	a.logs = logbuffer.New(1000)
	a.logs.HookStdLog("backend")
	log.Printf("startup: cfgDir=%s", cfgDir)
	bundleDir := filepath.Join(cfgDir, "skills")
	if _, err := bundle.Extract(bundleDir); err != nil {
		log.Printf("skill bundle extract: %v\n", err)
	}
	// Install (and refresh) as user-scoped Claude Code slash commands so the
	// spawned worker resolves /conductor-plan etc.
	if err := bundle.InstallAsCommands(); err != nil {
		log.Printf("skill install as commands: %v\n", err)
	}

	a.bus = eventbus.New()

	// rateLimitSink persists rate-limit snapshots and notifies the frontend.
	// Captured as a closure so it can be wired to both the OpenAI HTTP path
	// and the Claude stream-event path without passing the App reference
	// through package boundaries. The nil-store guard handles the unlikely
	// race at startup before the store is open.
	rateLimitSink := func(usages []types.PoolUsage) {
		if a.store == nil {
			return
		}
		for _, u := range usages {
			if err := a.store.UpsertPoolUsage(u); err != nil {
				log.Printf("upsert pool usage: %v", err)
			}
		}
		if a.ctx != nil {
			wruntime.EventsEmit(a.ctx, "pool.usage_updated", nil)
		}
	}

	a.providers = llm.NewRegistry(
		llm.NewClaudeProvider(),
		llm.NewOpenAIProviderWithSink(rateLimitSink),
		llm.NewLiteLLMProvider(),
		llm.NewLMStudioProvider(),
		llm.NewOllamaProvider(),
		llm.NewGeminiProvider(),
	)
	a.poolReg = workerpool.NewRegistry(a.providers.CanSpawn)
	a.orch = orchestrator.New(a.bus, a.resolveOrchestratorLLM)
	a.auth = githubauth.New("")

	if s, err := store.Open(cfgDir); err != nil {
		log.Printf("store open: %v\n", err)
	} else {
		a.store = s
		_ = a.store.EnsureDepCacheTable()
		if v, _ := a.store.GetSetting("auto_pull_paused"); v == "true" {
			a.orch.SetPaused(true)
		}
		// Issue #27: seed one Claude pool from the legacy worker_pool_capacity
		// setting on first run. Subsequent runs read whatever the user has
		// configured in Settings → Pools.
		if n, err := a.store.PoolsCount(); err == nil && n == 0 {
			a.migrateClaudePoolFromLegacy()
		}
		// Issue #39: migrate the legacy ollama_url / ollama_model settings into
		// a role=orchestrator pool. Idempotent; no-op if a pool already exists
		// or if no legacy keys are set (fresh install).
		if err := a.migrateOrchestratorPool(); err != nil {
			log.Printf("migrate orchestrator pool: %v", err)
		}
		if rows, err := a.store.ListPools(); err == nil {
			a.poolReg.Sync(rows)
		} else {
			log.Printf("pools sync: list: %v", err)
		}
	}
	if r, err := workspace.New(cfgDir); err != nil {
		log.Printf("workspace registry: %v\n", err)
	} else {
		a.wsReg = r
		// Issue #92: one-time migration of legacy Mode/UseConductor* → PerStage.
		a.migrateWorkspaceSkillProfiles()
		// Issue #192: remove workspace rows left in provisioning state by the
		// pre-fix wizard or by a crash mid-provision.
		if stale := a.wsReg.ReconcileProvisioning(); len(stale) > 0 {
			a.emitToast("info", "Workspace Cleanup", fmt.Sprintf("Removed %d stale provisioning workspace(s): %v", len(stale), stale), nil)
		}
	}

	// Issue #22: prune any orphan worktrees from prior conductor sessions, then
	// remove worktrees for terminal-state sessions whose ended_at is older than
	// 24h. Repeated on a 1h tick (q3=A) so a long-running app session catches
	// post-success worktrees that aged in.
	if a.wsReg != nil {
		a.gcWorktreesAll()
		go func() {
			t := time.NewTicker(1 * time.Hour)
			defer t.Stop()
			for {
				select {
				case <-a.ctx.Done():
					return
				case <-t.C:
					a.gcWorktreesAll()
				}
			}
		}()
	}

	// Issue #161: ephemeral PTY-backed agent terminal panel.
	a.agentTerm = agentterm.New(a.emitAgentData, a.emitAgentExit)

	a.mgr = session.NewManager(a.bus, a.emitLine)
	a.mgr.Configure(filepath.Join(cfgDir, "transcripts"), a.store, a.handleSessionStateChange)
	a.mgr.SetProviders(a.providers)
	a.mgr.SetOnPlanReady(a.handlePlanReady)
	a.mgr.SetOnPROpened(a.handlePROpened)
	a.mgr.SetOnNeedsPR(a.handleNeedsPR)
	a.mgr.SetOnActivity(func(act types.SessionActivity) {
		wruntime.EventsEmit(a.ctx, "session.activity", act)
	})
	a.mgr.SetOnRateLimit(rateLimitSink)

	// Hook the orchestrator up to the store + pool + spawn callback now that
	// every dependency exists.
	if a.store != nil {
		a.orch.SetStore(a.store)
		a.orch.SetAutoPull(a.poolReg, func(workspaceID string, issueNumber int, poolID string) error {
			ws, ok := a.wsReg.Get(workspaceID)
			if !ok {
				return fmt.Errorf("unknown workspace %q", workspaceID)
			}
			pool, err := a.store.GetPool(poolID)
			if err != nil {
				return fmt.Errorf("resolve pool %q: %w", poolID, err)
			}
			_, err = a.mgr.SpawnPlan(ws, types.Issue{Number: issueNumber, WorkspaceID: workspaceID}, pool)
			return err
		})
		// Issue #39: feed the registry the workspace's SkillProfile so
		// PreferredPlanPoolID pinning works in auto-pull.
		if a.wsReg != nil {
			a.orch.SetWorkspaceLookup(func(id string) (types.Workspace, bool) {
				return a.wsReg.Get(id)
			})
		}

		// Issue #40: wire work-role spawn for pending-pool drain (approve_execute
		// requests that were queued because no work pool had free capacity).
		if a.wsReg != nil {
			a.orch.SetSpawnWork(func(workspaceID string, issueNumber int, poolID string) error {
				ws, ok := a.wsReg.Get(workspaceID)
				if !ok {
					return fmt.Errorf("unknown workspace %q", workspaceID)
				}
				pool, err := a.store.GetPool(poolID)
				if err != nil {
					return fmt.Errorf("resolve pool %q: %w", poolID, err)
				}
				issue, err := a.store.LoadIssue(workspaceID, issueNumber)
				if err != nil {
					return fmt.Errorf("load issue #%d: %w", issueNumber, err)
				}
				plan, err := a.store.LatestPlan(workspaceID, issueNumber)
				if err != nil || plan == nil {
					return fmt.Errorf("load plan for #%d: %w", issueNumber, err)
				}
				if err := a.store.MoveIssueColumn(workspaceID, issueNumber, types.ColInProgress); err != nil {
					return fmt.Errorf("move column #%d: %w", issueNumber, err)
				}
				_, err = a.mgr.SpawnExecute(ws, issue, *plan, pool)
				if err != nil {
					_ = a.store.MoveIssueColumn(workspaceID, issueNumber, types.ColPlan)
					a.poolReg.ReleaseByPool(poolID)
				}
				return err
			})
		}

		// Issue #40: drain any pending pool requests from before this startup
		// so queued work survives conductor restarts.
		go a.orch.KickDrain("startup")

		// Persist every event for debugging + Phase 7 transcript pattern detector.
		a.bus.Subscribe(func(e eventbus.Event) {
			_ = a.store.LogEvent(string(e.Type), e.Payload)
			wruntime.EventsEmit(a.ctx, "bus."+string(e.Type), e.Payload)
			a.notifyOnPRStateChange(e)
		})

		// Issue #98: canonical IssueView assembler — emits bus.issue_view_updated
		// whenever any contributing source changes.
		a.assembler = issueview.New(a.bus, a.store)
		// Issue #153: wire workspace path resolver so the assembler can probe
		// for orphan question files during IssueView assembly.
		if a.wsReg != nil {
			a.assembler.SetWorkspacePathFn(func(id string) string {
				if ws, ok := a.wsReg.Get(id); ok {
					return ws.RepoPath
				}
				return ""
			})
		}

		// Pool counter self-heal: on every terminal session-state event,
		// re-derive every pool's `active` counter from the DB count of
		// running sessions. This makes the registry counter a cache of
		// authoritative DB state instead of a hand-maintained ledger that
		// leaks slots on every acquire-without-release path. The hot
		// AcquireForPlan / AcquireForWork path still uses the in-memory
		// counter (synchronous, no DB round-trip), but we promise to keep
		// it correct on the next event tick.
		a.bus.Subscribe(func(e eventbus.Event) {
			switch e.Type {
			case eventbus.EvtSessionStateChanged, eventbus.EvtWorkerSlotFreed:
				a.reconcilePoolActiveCounters(string(e.Type))
			}
		})

		// Issue #116: auto-spawn self-heal and toast on CI check failures.
		a.bus.Subscribe(a.handlePRChecksFailed)
		a.bus.Subscribe(a.handlePRChecksRecovered)

		// Issue #124: PR merge-conflict detection and resolution worker.
		a.bus.Subscribe(a.handlePRConflictsDetected)
		a.bus.Subscribe(a.handlePRConflictsResolved)
	}

	// Issue #17: answer-file watcher. Polls each enabled workspace's
	// .prismconductor/answers/ directory on a 1s ticker; when an answer file
	// matches a session paused on that question, fire the resume callback. The
	// watcher does NOT distinguish "answer arrived live" from "answer was
	// already on disk at startup" — the first tick post-restart auto-resumes
	// any pre-existing match (Q4=A).
	if a.store != nil && a.wsReg != nil {
		a.answerWatcher = store.NewAnswerWatcher(
			time.Second,
			func() []types.Workspace { return a.wsReg.List() },
			func() []store.PausedSession {
				ps, err := a.store.ListPausedForQuestionSessions()
				if err != nil {
					log.Printf("answer watcher: list paused sessions: %v", err)
					return nil
				}
				return ps
			},
			a.handleMidRunAnswerArrived,
		)
		// Issue #153: auto-recover orphaned paused sessions after 60s grace.
		a.answerWatcher.SetOrphanCallback(60*time.Second, a.handleOrphanQuestion)
		go a.answerWatcher.Run(a.ctx)
	}

	// Re-attach to sessions that were live at last shutdown (§15.3).
	// Issue #54: Reattach now needs the workspace list so the live-tail
	// goroutine can rebuild repoPath / worktreeDir for post-mortem cleanup.
	// Issue #105: wire the pool rehydrator so live-session slots are counted.
	if a.poolReg != nil {
		a.mgr.SetPoolRehydrator(a.poolReg)
	}
	if a.store != nil {
		if running, _, err := a.store.LoadRunningSessions(); err == nil {
			var workspaces []types.Workspace
			if a.wsReg != nil {
				workspaces = a.wsReg.List()
			}
			a.mgr.Reattach(running, workspaces)
		}
		// Self-heal any closed issues stuck in non-DONE columns from earlier
		// buggy poll cycles.
		if n, err := a.store.ReconcileClosedIssues(); err != nil {
			log.Printf("reconcile closed issues: %v\n", err)
		} else if n > 0 {
			log.Printf("reconciled %d closed issues to DONE\n", n)
		}
		// Self-heal any session row left in state=running for an issue whose
		// PR is already open or merged. Harness sessions can leave stale
		// running rows when their goroutine dies/hangs without cleaning up;
		// without this pass the assembler picks them as active_session and
		// the card glows blue/purple in REVIEW/DONE forever (witnessed on
		// issue #109).
		if n, err := a.store.ReconcileStaleRunningSessions(); err != nil {
			log.Printf("reconcile stale running sessions: %v\n", err)
		} else if n > 0 {
			log.Printf("reconciled %d stale running sessions to failed\n", n)
		}

		// Self-heal stuck waiting_for_pool flags + leftover
		// pending_pool_for rows on REVIEW/DONE cards. Witnessed on #118:
		// PR merged, card moved to DONE, but waiting_for_pool=1 + a stale
		// pending_pool_for row from the original approve-execute enqueue
		// stuck around → card glowed pink "waiting for available agent
		// pool" in DONE forever. The fix in MarkPRMerged /
		// MarkPRClosedUnmerged prevents future leaks; this reconciles
		// any pre-fix data that's already in this state.
		if iss, q, err := a.store.ReconcileStalePoolQueueForClosedIssues(); err != nil {
			log.Printf("reconcile stale pool queue: %v\n", err)
		} else if iss > 0 || q > 0 {
			log.Printf("reconciled stale pool-queue state: cleared waiting_for_pool on %d closed/review issues, deleted %d pending_pool_for rows\n", iss, q)
		}

		// Reconcile pool registry's in-memory `active` counter against DB
		// truth. The hand-maintained TryAcquire/Release counter leaks on
		// any acquire-without-matching-release (witnessed: planner 1/2
		// with zero plan sessions running). DB count is authoritative;
		// registry becomes a cache derived from it. Subsequent terminal
		// session-state events keep it fresh via the bus subscriber wired
		// in startup.go.
		a.reconcilePoolActiveCounters("startup")

		// Issue #107: auto-archive DONE cards per workspace config. Run once at
		// startup and then on a 24-hour tick so long-running sessions stay clean.
		go func() {
			a.runAutoArchiveAll()
			t := time.NewTicker(24 * time.Hour)
			defer t.Stop()
			for {
				select {
				case <-a.ctx.Done():
					return
				case <-t.C:
					a.runAutoArchiveAll()
				}
			}
		}()
	}

	// GitHub poller (#2). Uses the existing `gh` CLI auth — no OAuth App
	// registration required.
	if a.store != nil && a.wsReg != nil {
		if c, err := pcgithub.New(); err != nil {
			log.Printf("github client unavailable: %v\n", err)
		} else {
			a.gh = c
			interval := 5 * time.Minute
			if v, _ := a.store.GetSetting("poll_interval_seconds"); v != "" {
				if n, err := strconv.Atoi(v); err == nil && n >= 30 {
					interval = time.Duration(n) * time.Second
				}
			}
			a.poller = pcgithub.NewPoller(a.bus, a.gh, a.store, a.wsReg, interval)
			go a.poller.Run(a.ctx)
		}
	}
}

// handleSessionStateChange runs on every session state transition. Fans the
// new state to the UI as a Wails event and fires an in-app toast per §15.6.
func (a *App) handleSessionStateChange(sess types.Session, prev types.SessionState) {
	wruntime.EventsEmit(a.ctx, "session.state", sess)
	a.bus.Publish(eventbus.EvtSessionStateChanged, eventbus.SessionStateChanged{
		WorkspaceID: sess.WorkspaceID,
		IssueNumber: sess.IssueNumber,
		SessionID:   sess.ID,
	})

	if prev == sess.State {
		return
	}

	// When an execute session fails, persist the failure reason on the issue
	// and move the card to the BLOCKED column so it appears in a dedicated
	// repair column instead of staying silently in IN_PROGRESS (#194).
	if sess.Mode == types.ModeExecute &&
		(sess.State == types.StateBlocked || sess.State == types.StateFailed) &&
		a.store != nil {
		reason := sess.BlockedReason
		if reason == "" {
			reason = "execute session ended without success"
		}
		_ = a.store.SetIssueFailureReason(sess.WorkspaceID, sess.IssueNumber, reason)
	}

	// When an execute session COMPLETES on a card that had merge conflicts
	// recorded, optimistically clear the conflict info so the card doesn't
	// stay glowing red after the resolver demonstrably finished. The
	// poller's next cycle will re-detect via probeConflicts if the
	// resolver actually didn't fix it. Without this, the badge persists
	// for up to 5 minutes after the resolver session terminates because
	// EvtPRConflictsResolved only fires when GitHub's mergeable_state
	// transitions out of "dirty" — which the conductor only learns about
	// at the next poll tick. Witnessed on issue #177.
	if sess.Mode == types.ModeExecute && sess.State == types.StateCompleted &&
		a.assembler != nil && a.assembler.HasConflictsInfo(sess.WorkspaceID, sess.IssueNumber) {
		a.assembler.ClearConflictsInfo(sess.WorkspaceID, sess.IssueNumber)
		// Kick the poller for definitive verification on the next cycle.
		// If the conflict actually persists, probeConflicts will re-fire
		// EvtPRConflictsDetected and the badge comes back. If it's gone,
		// nothing happens and the badge stays cleared.
		if a.poller != nil {
			a.poller.PokeNow()
		}
	}
	switch sess.State {
	case types.StateWaitingForInput, types.StatePausedForQuestion, types.StateBlocked, types.StateCompleted, types.StateFailed:
		// One notification per transition; dedupe identical kicks within 2s.
		key := sess.ID + ":" + string(sess.State)
		a.notifyMu.Lock()
		now := nowUnix()
		if key == a.lastNotifyKey && now-a.lastNotifyAt < 2 {
			a.notifyMu.Unlock()
			return
		}
		a.lastNotifyKey = key
		a.lastNotifyAt = now
		a.notifyMu.Unlock()

		if a.notificationsSuppressed() {
			return
		}

		var level string
		switch sess.State {
		case types.StateBlocked, types.StateFailed:
			level = "error"
		case types.StateWaitingForInput, types.StatePausedForQuestion:
			level = "warning"
		case types.StateCompleted:
			level = "info"
		default:
			return
		}
		a.emitToast(level, toastWorkspaceName(a.wsReg, sess.WorkspaceID), notifyBody(sess), map[string]any{
			"workspace_id": sess.WorkspaceID,
			"issue_number": sess.IssueNumber,
			"action":       "focus_card",
		})
	}
}

// notificationsSuppressed returns true when notifications are currently muted
// or inside the user's quiet-hours window (§15.6).
func (a *App) notificationsSuppressed() bool {
	if a.store == nil {
		return false
	}
	if v, _ := a.store.GetSetting("notify_muted"); v == "true" {
		return true
	}
	startStr, _ := a.store.GetSetting("notify_quiet_start")
	endStr, _ := a.store.GetSetting("notify_quiet_end")
	if startStr == "" || endStr == "" {
		return false
	}
	start, ok1 := parseHHMM(startStr)
	end, ok2 := parseHHMM(endStr)
	if !ok1 || !ok2 || start == end {
		return false
	}
	now := time.Now()
	curMin := now.Hour()*60 + now.Minute()
	if start < end {
		return curMin >= start && curMin < end
	}
	// Wraps midnight (e.g., 22:00 → 07:00).
	return curMin >= start || curMin < end
}

func parseHHMM(s string) (int, bool) {
	if len(s) != 5 || s[2] != ':' {
		return 0, false
	}
	h, err1 := strconv.Atoi(s[:2])
	m, err2 := strconv.Atoi(s[3:])
	if err1 != nil || err2 != nil || h < 0 || h > 23 || m < 0 || m > 59 {
		return 0, false
	}
	return h*60 + m, true
}

// NotifyPrefs is the user-visible notification config.
type NotifyPrefs struct {
	Muted      bool   `json:"muted"`
	QuietStart string `json:"quiet_start"` // "HH:MM"
	QuietEnd   string `json:"quiet_end"`   // "HH:MM"
}

func (a *App) GetNotifyPrefs() NotifyPrefs {
	out := NotifyPrefs{}
	if a.store == nil {
		return out
	}
	if v, _ := a.store.GetSetting("notify_muted"); v == "true" {
		out.Muted = true
	}
	out.QuietStart, _ = a.store.GetSetting("notify_quiet_start")
	out.QuietEnd, _ = a.store.GetSetting("notify_quiet_end")
	return out
}

func (a *App) SetNotifyPrefs(p NotifyPrefs) error {
	if a.store == nil {
		return fmt.Errorf("store unavailable")
	}
	muted := "false"
	if p.Muted {
		muted = "true"
	}
	_ = a.store.SetSetting("notify_muted", muted)
	_ = a.store.SetSetting("notify_quiet_start", p.QuietStart)
	_ = a.store.SetSetting("notify_quiet_end", p.QuietEnd)
	return nil
}

// toastWorkspaceName returns the workspace's display name for use as a toast
// title. Falls back to "PrismConductor" when the workspace ID is empty/unknown.
// Toasts render inside the app frame, so the long-form "PrismConductor — X"
// title from the prior osascript path is no longer needed.
func toastWorkspaceName(reg *workspace.Registry, id string) string {
	if reg == nil || id == "" {
		return "PrismConductor"
	}
	if ws, ok := reg.Get(id); ok {
		return ws.DisplayName
	}
	return "PrismConductor"
}

func notifyBody(sess types.Session) string {
	prefix := ""
	if sess.IssueNumber > 0 {
		prefix = fmt.Sprintf("#%d ", sess.IssueNumber)
	}
	switch sess.State {
	case types.StateWaitingForInput:
		return prefix + "needs input"
	case types.StatePausedForQuestion:
		return prefix + "needs answer mid-run"
	case types.StateBlocked:
		return prefix + "is blocked"
	case types.StateCompleted:
		return prefix + "completed"
	case types.StateFailed:
		return prefix + "failed"
	}
	return prefix + string(sess.State)
}

func nowUnix() int64 {
	return time.Now().Unix()
}

// emitLine fans PTY output out as a Wails event for the frontend SessionDrawer.
func (a *App) emitLine(sessionID, line string) {
	wruntime.EventsEmit(a.ctx, "session.line", map[string]string{
		"session_id": sessionID,
		"line":       line,
	})
}

// emitToast sends a transient in-app toast to the frontend via Wails event.
// Replaces the old osascript-backed notify.Send (issue #32). The level drives
// styling on the frontend; payload carries click-target metadata
// (workspace_id, issue_number, optional pr_url, action).
func (a *App) emitToast(level, title, body string, payload map[string]any) {
	if a.ctx == nil {
		return
	}
	msg := map[string]any{
		"id":    uuid.NewString(),
		"level": level,
		"title": title,
		"body":  body,
	}
	for k, v := range payload {
		msg[k] = v
	}
	wruntime.EventsEmit(a.ctx, "toast", msg)
}

// gcWorktreesAll prunes orphan worktree records and removes any
// `.prismconductor/worktrees/<wsID>-<num>` directory whose most recent
// terminal-state session ended more than 24h ago. Issue #22, q3=A.
func (a *App) gcWorktreesAll() {
	if a.wsReg == nil {
		return
	}
	cutoff := time.Now().Add(-24 * time.Hour)
	for _, ws := range a.wsReg.List() {
		if ws.RepoPath == "" {
			continue
		}
		if err := pcgit.Prune(ws.RepoPath); err != nil {
			log.Printf("worktree prune %s: %v", ws.ID, err)
		}
		entries, err := pcgit.List(ws.RepoPath)
		if err != nil {
			continue
		}
		prefix := filepath.Join(ws.RepoPath, ".prismconductor", "worktrees")
		for _, e := range entries {
			if !strings.HasPrefix(e.Path, prefix) {
				continue
			}
			if a.store == nil {
				continue
			}
			ended, ok, err := a.store.MostRecentEndedAtForWorktree(ws.ID, e.Path)
			if err != nil || !ok {
				continue
			}
			if ended.Before(cutoff) {
				if err := pcgit.Remove(ws.RepoPath, e.Path); err != nil {
					log.Printf("24h GC remove %s: %v", e.Path, err)
				}
			}
		}
	}
}

// GCWorktrees force-removes every conductor-managed worktree under
// `<RepoPath>/.prismconductor/worktrees/` for one workspace, regardless of
// session state. Surfaced as a manual "something got jammed" recovery in the
// Workspaces panel (issue #22). Returns the count of directories removed.
func (a *App) GCWorktrees(workspaceID string) (int, error) {
	if a.wsReg == nil {
		return 0, fmt.Errorf("workspace registry unavailable")
	}
	ws, ok := a.wsReg.Get(workspaceID)
	if !ok {
		return 0, fmt.Errorf("unknown workspace %q", workspaceID)
	}
	entries, err := pcgit.List(ws.RepoPath)
	if err != nil {
		return 0, err
	}
	prefix := filepath.Join(ws.RepoPath, ".prismconductor", "worktrees")
	removed := 0
	for _, e := range entries {
		if !strings.HasPrefix(e.Path, prefix) {
			continue
		}
		if err := pcgit.Remove(ws.RepoPath, e.Path); err != nil {
			log.Printf("GCWorktrees remove %s: %v", e.Path, err)
			continue
		}
		removed++
	}
	if err := pcgit.Prune(ws.RepoPath); err != nil {
		log.Printf("GCWorktrees prune %s: %v", ws.ID, err)
	}
	return removed, nil
}

// --- Bound methods exposed to frontend ---

// ListWorkspaces returns the registered workspaces.
func (a *App) ListWorkspaces() []types.Workspace {
	if a.wsReg == nil {
		return nil
	}
	return a.wsReg.List()
}

// OnboardCheck runs the §3 onboarding checks against a path.
func (a *App) OnboardCheck(path string) []workspace.OnboardCheck {
	return workspace.Onboard(path)
}

// InspectRepo returns onboarding checks + parsed owner/repo + skill profile + conventions
// for a candidate path. Used to pre-fill the Add Workspace form.
func (a *App) InspectRepo(path string) workspace.Inspection {
	return workspace.Inspect(path)
}

// PickRepoPath opens the native folder picker.
func (a *App) PickRepoPath() (string, error) {
	return wruntime.OpenDirectoryDialog(a.ctx, wruntime.OpenDialogOptions{
		Title: "Select repo for workspace",
	})
}

// AddWorkspace registers a new workspace, then triggers an immediate issue fetch
// and emits EvtWorkspaceAdded so the frontend can auto-select and show a toast.
func (a *App) AddWorkspace(ws types.Workspace) error {
	if a.wsReg == nil {
		return fmt.Errorf("workspace registry unavailable")
	}
	if !ws.Enabled {
		ws.Enabled = true
	}
	if err := a.wsReg.Add(ws); err != nil {
		return err
	}
	if a.bus != nil {
		a.bus.Publish(eventbus.EvtWorkspaceAdded, map[string]any{
			"workspace_id":   ws.ID,
			"workspace_name": ws.DisplayName,
		})
	}
	if a.poller != nil {
		go a.poller.FetchNow(a.ctx, ws)
	}
	return nil
}

// UpdateWorkspace replaces a workspace's record.
func (a *App) UpdateWorkspace(ws types.Workspace) error {
	if a.wsReg == nil {
		return fmt.Errorf("workspace registry unavailable")
	}
	return a.wsReg.Update(ws)
}

// RemoveWorkspace removes a workspace by ID. Any pools bound to this workspace
// are silently rebound to shared so capacity isn't orphaned (issue #109, q1).
// For remote workspaces the CF API token is removed from the OS keyring (issue #178).
// Collection membership is cleaned up before the workspace row is removed (#209).
func (a *App) RemoveWorkspace(id string) error {
	if a.wsReg == nil {
		return fmt.Errorf("workspace registry unavailable")
	}
	if ws, ok := a.wsReg.Get(id); ok {
		workspace.RemoteCleanup(ws, a.secretStore)
		if ws.RemoteConfig != nil {
			if err := remoteworker.DeleteKey(id); err != nil {
				log.Printf("RemoveWorkspace: delete API key for %s: %v", id, err)
			}
		}
	}
	if a.store != nil {
		if err := a.store.RemoveWorkspaceFromAllCollections(id); err != nil {
			log.Printf("RemoveWorkspace: unlink from collections for %s: %v", id, err)
		}
		if err := a.store.RebindWorkspacePools(id); err != nil {
			log.Printf("RemoveWorkspace: rebind pools for %s: %v", id, err)
		} else if a.poolReg != nil {
			if rows, err := a.store.ListPools(); err == nil {
				a.poolReg.Sync(rows)
			}
		}
	}
	return a.wsReg.Remove(id)
}

// --- Collections (issue #209) ---

// ListCollections returns all workspace collections.
func (a *App) ListCollections() ([]types.Collection, error) {
	if a.store == nil {
		return nil, fmt.Errorf("store unavailable")
	}
	return a.store.ListCollections()
}

// CreateCollection creates a new workspace collection.
func (a *App) CreateCollection(name string) (types.Collection, error) {
	if a.store == nil {
		return types.Collection{}, fmt.Errorf("store unavailable")
	}
	col, err := a.store.CreateCollection(name)
	if err != nil {
		return types.Collection{}, err
	}
	if a.bus != nil {
		a.bus.Publish(eventbus.EvtCollectionsUpdated, nil)
	}
	if a.ctx != nil {
		wruntime.EventsEmit(a.ctx, "collections.updated", nil)
	}
	return col, nil
}

// RenameCollection updates a collection's display name.
func (a *App) RenameCollection(id, name string) error {
	if a.store == nil {
		return fmt.Errorf("store unavailable")
	}
	if err := a.store.RenameCollection(id, name); err != nil {
		return err
	}
	if a.bus != nil {
		a.bus.Publish(eventbus.EvtCollectionsUpdated, nil)
	}
	if a.ctx != nil {
		wruntime.EventsEmit(a.ctx, "collections.updated", nil)
	}
	return nil
}

// DeleteCollection removes a collection and all its membership rows.
func (a *App) DeleteCollection(id string) error {
	if a.store == nil {
		return fmt.Errorf("store unavailable")
	}
	if err := a.store.DeleteCollection(id); err != nil {
		return err
	}
	if a.bus != nil {
		a.bus.Publish(eventbus.EvtCollectionsUpdated, nil)
	}
	if a.ctx != nil {
		wruntime.EventsEmit(a.ctx, "collections.updated", nil)
	}
	return nil
}

// AddWorkspaceToCollection adds a workspace to a collection.
func (a *App) AddWorkspaceToCollection(collectionID, workspaceID string) error {
	if a.store == nil {
		return fmt.Errorf("store unavailable")
	}
	if err := a.store.AddWorkspaceToCollection(collectionID, workspaceID); err != nil {
		return err
	}
	if a.bus != nil {
		a.bus.Publish(eventbus.EvtCollectionsUpdated, nil)
	}
	if a.ctx != nil {
		wruntime.EventsEmit(a.ctx, "collections.updated", nil)
	}
	return nil
}

// RemoveWorkspaceFromCollection removes a workspace from a collection.
func (a *App) RemoveWorkspaceFromCollection(collectionID, workspaceID string) error {
	if a.store == nil {
		return fmt.Errorf("store unavailable")
	}
	if err := a.store.RemoveWorkspaceFromCollection(collectionID, workspaceID); err != nil {
		return err
	}
	if a.bus != nil {
		a.bus.Publish(eventbus.EvtCollectionsUpdated, nil)
	}
	if a.ctx != nil {
		wruntime.EventsEmit(a.ctx, "collections.updated", nil)
	}
	return nil
}

// UpdateCollectionContext replaces the shared context markdown for a collection.
func (a *App) UpdateCollectionContext(collectionID, body string) error {
	if a.store == nil {
		return fmt.Errorf("store unavailable")
	}
	if err := a.store.UpdateCollectionContext(collectionID, body); err != nil {
		return err
	}
	if a.bus != nil {
		a.bus.Publish(eventbus.EvtCollectionsUpdated, nil)
	}
	if a.ctx != nil {
		wruntime.EventsEmit(a.ctx, "collections.updated", nil)
	}
	return nil
}

// --- Remote workspace helpers (issue #171) ---

// CFTokenResult is the frontend-visible result of TestCloudflareToken.
type CFTokenResult struct {
	AccountID   string `json:"account_id"`
	AccountName string `json:"account_name,omitempty"`
}

// TestCloudflareToken verifies a Cloudflare API token and returns the account
// ID. Called by RemoteWorkspaceSetup before persisting anything.
func (a *App) TestCloudflareToken(token string) (CFTokenResult, error) {
	res, err := remoteworker.VerifyToken(token)
	if err != nil {
		return CFTokenResult{}, err
	}
	return CFTokenResult{AccountID: res.AccountID, AccountName: res.AccountName}, nil
}

// TestGitHubPAT verifies that a GitHub Personal Access Token has push access
// to owner/repo. Called by RemoteWorkspaceSetup before persisting the PAT ref.
func (a *App) TestGitHubPAT(pat, owner, repo string) error {
	return remoteworker.VerifyGitHubPAT(pat, owner, repo)
}

// GetRepoDefaultBranch returns the default branch of owner/repo as reported
// by the GitHub API. Called by the remote-workspace wizard after the user
// pastes a repository URL so the branch field can be pre-filled.
func (a *App) GetRepoDefaultBranch(pat, owner, repo string) (string, error) {
	return remoteworker.GetRepoDefaultBranch(pat, owner, repo)
}

// RemoteWorkspaceForm is the input to CreateRemoteWorkspace — all fields
// required to provision a remote workspace end-to-end in a single call.
type RemoteWorkspaceForm struct {
	// Credentials (never stored locally)
	CFToken   string `json:"cf_token"`
	GitHubPAT string `json:"github_pat"`
	// Workspace identity
	WorkspaceID   string `json:"workspace_id"`
	DisplayName   string `json:"display_name"`
	GitHubOwner   string `json:"github_owner"`
	GitHubRepo    string `json:"github_repo"`
	DefaultBranch string `json:"default_branch"`
	Color         string `json:"color"`
}

// CreateRemoteWorkspace provisions a Cloudflare Worker and registers the
// workspace in a single atomic call. The registry row is written ONLY after
// all remote steps succeed, so a failure at any point leaves no zombie row.
// Best-effort cleanup of the CF worker is attempted if any post-deploy step fails.
func (a *App) CreateRemoteWorkspace(form RemoteWorkspaceForm) (RemoteDeployResult, error) {
	if a.wsReg == nil {
		return RemoteDeployResult{}, fmt.Errorf("workspace registry unavailable")
	}
	if form.WorkspaceID == "" {
		return RemoteDeployResult{}, fmt.Errorf("workspace ID required")
	}

	// Verify CF token and resolve account ID.
	tkRes, err := remoteworker.VerifyToken(form.CFToken)
	if err != nil {
		return RemoteDeployResult{}, fmt.Errorf("CF token invalid: %w", err)
	}
	accountID := tkRes.AccountID

	// Verify GitHub PAT has push access.
	if err := remoteworker.VerifyGitHubPAT(form.GitHubPAT, form.GitHubOwner, form.GitHubRepo); err != nil {
		return RemoteDeployResult{}, fmt.Errorf("GitHub PAT invalid: %w", err)
	}

	// Deploy the worker bundle. From here on, failures should attempt cleanup.
	deploy, err := remoteworker.DeployWorker(accountID, form.CFToken, form.WorkspaceID, remoteworker.WorkerBundle)
	if err != nil {
		return RemoteDeployResult{}, fmt.Errorf("deploy worker: %w", err)
	}

	// bestEffortCleanup deletes the CF worker if a post-deploy step fails.
	bestEffortCleanup := func(reason string) {
		if cleanErr := remoteworker.DeleteWorker(accountID, form.CFToken, deploy.WorkerName); cleanErr != nil {
			log.Printf("CreateRemoteWorkspace: best-effort cleanup of %s after %s: %v (non-fatal)", deploy.WorkerName, reason, cleanErr)
		} else {
			log.Printf("CreateRemoteWorkspace: cleaned up CF worker %s after %s", deploy.WorkerName, reason)
		}
	}

	// Store GitHub PAT as a CF Secret.
	if err := remoteworker.UpsertSecret(accountID, form.CFToken, deploy.WorkerName, "GITHUB_PAT", form.GitHubPAT); err != nil {
		bestEffortCleanup("github_pat_secret_fail")
		return RemoteDeployResult{}, fmt.Errorf("store GitHub PAT secret: %w", err)
	}

	// Persist the CF API token in the OS keyring.
	tokenKey := secretstore.CFTokenKey(form.WorkspaceID)
	var keyringUnavailable bool
	if err := a.secretStore.Set(tokenKey, form.CFToken); err != nil {
		if errors.Is(err, secretstore.ErrKeyringUnavailable) {
			log.Printf("CreateRemoteWorkspace: OS keyring unavailable for %s; token not stored (user consent required for file fallback)", form.WorkspaceID)
			keyringUnavailable = true
			tokenKey = ""
		} else {
			log.Printf("CreateRemoteWorkspace: keyring set for %s: %v (non-fatal, token not persisted)", form.WorkspaceID, err)
			tokenKey = ""
		}
	}

	// Generate and persist the conductor API key.
	apiKey, err := randomKey256()
	if err != nil {
		bestEffortCleanup("api_key_gen_fail")
		return RemoteDeployResult{}, fmt.Errorf("generate conductor API key: %w", err)
	}
	if err := remoteworker.UpsertSecret(accountID, form.CFToken, deploy.WorkerName, "CONDUCTOR_API_KEY", apiKey); err != nil {
		bestEffortCleanup("conductor_key_secret_fail")
		return RemoteDeployResult{}, fmt.Errorf("store conductor API key secret: %w", err)
	}
	warn, err := remoteworker.SetKey(form.WorkspaceID, apiKey)
	if err != nil {
		bestEffortCleanup("conductor_key_local_fail")
		return RemoteDeployResult{}, fmt.Errorf("store conductor API key locally: %w", err)
	}
	if warn != "" {
		a.emitToast("warning", "Remote Workspace", warn, nil)
	}

	// All remote steps succeeded — now write the registry row.
	ws := types.Workspace{
		ID:            form.WorkspaceID,
		DisplayName:   form.DisplayName,
		GitHubOwner:   form.GitHubOwner,
		GitHubRepo:    form.GitHubRepo,
		DefaultBranch: form.DefaultBranch,
		Color:         form.Color,
		AgentEnv:      types.EnvSpec{EnvVars: map[string]string{}, Shell: "/bin/bash"},
		SkillProfile:  types.SkillProfile{Mode: types.SkillModeBundled},
		Enabled:       true,
		ExecutionTarget: types.ExecutionTargetRemote,
		RemoteConfig: &types.RemoteConfig{
			CFAccountID:         accountID,
			CFWorkerName:        deploy.WorkerName,
			CFWorkerEndpointURL: deploy.CFWorkerEndpointURL,
			CFDeploymentVersion: deploy.DeploymentVersion,
			SecretRefs: types.RemoteSecretRefs{
				GitHubPATRef:       "GITHUB_PAT",
				CFAPITokenRef:      tokenKey,
				ConductorAPIKeyRef: "CONDUCTOR_API_KEY",
			},
		},
	}
	if err := a.wsReg.Add(ws); err != nil {
		bestEffortCleanup("registry_add_fail")
		_ = remoteworker.DeleteKey(form.WorkspaceID)
		return RemoteDeployResult{}, fmt.Errorf("register workspace: %w", err)
	}

	if a.bus != nil {
		a.bus.Publish(eventbus.EvtWorkspaceAdded, map[string]any{
			"workspace_id":   ws.ID,
			"workspace_name": ws.DisplayName,
		})
	}
	if a.poller != nil {
		go a.poller.FetchNow(a.ctx, ws)
	}

	return RemoteDeployResult{
		WorkerName:          deploy.WorkerName,
		CFWorkerEndpointURL: deploy.CFWorkerEndpointURL,
		DeploymentVersion:   deploy.DeploymentVersion,
		KeyringUnavailable:  keyringUnavailable,
	}, nil
}

// RemoteDeployResult is the frontend-visible result of DeployRemoteWorker.
type RemoteDeployResult struct {
	WorkerName          string `json:"worker_name"`
	CFWorkerEndpointURL string `json:"cf_worker_endpoint_url"`
	DeploymentVersion   string `json:"deployment_version"`
	// KeyringUnavailable is true when the OS keyring could not store the CF
	// API token (headless Linux without a Secret Service daemon). The frontend
	// should offer an explicit file-fallback consent banner in this case and
	// call StoreCFTokenFileFallback if the user accepts.
	KeyringUnavailable bool `json:"keyring_unavailable,omitempty"`
}

// DeployRemoteWorker uploads the embedded worker bundle to the user's
// Cloudflare account and stores the GitHub PAT as a CF Secret. It returns the
// deployed worker's endpoint URL and version tag so the caller can persist them
// in the workspace's RemoteConfig.
//
// Raw tokens are NOT stored locally; only the CF Secret names (refs) are kept.
func (a *App) DeployRemoteWorker(workspaceID, cfToken, githubPAT string) (RemoteDeployResult, error) {
	if a.wsReg == nil {
		return RemoteDeployResult{}, fmt.Errorf("workspace registry unavailable")
	}
	ws, ok := a.wsReg.Get(workspaceID)
	if !ok {
		return RemoteDeployResult{}, fmt.Errorf("workspace %s not found", workspaceID)
	}

	// Verify token and resolve account ID.
	tkRes, err := remoteworker.VerifyToken(cfToken)
	if err != nil {
		return RemoteDeployResult{}, fmt.Errorf("CF token invalid: %w", err)
	}
	accountID := tkRes.AccountID

	// Deploy the worker bundle.
	deploy, err := remoteworker.DeployWorker(accountID, cfToken, workspaceID, remoteworker.WorkerBundle)
	if err != nil {
		return RemoteDeployResult{}, fmt.Errorf("deploy worker: %w", err)
	}

	// Store the GitHub PAT as a CF Secret (token value never touches disk).
	if err := remoteworker.UpsertSecret(accountID, cfToken, deploy.WorkerName, "GITHUB_PAT", githubPAT); err != nil {
		return RemoteDeployResult{}, fmt.Errorf("store GitHub PAT secret: %w", err)
	}

	// Persist the CF API token in the OS keyring so subsequent CF operations
	// can retrieve it without prompting the user again. The token value never
	// touches the DB or logs — only the namespaced key name (the "ref") is
	// persisted in the workspace metadata.
	tokenKey := secretstore.CFTokenKey(workspaceID)
	var keyringUnavailable bool
	if err := a.secretStore.Set(tokenKey, cfToken); err != nil {
		if errors.Is(err, secretstore.ErrKeyringUnavailable) {
			log.Printf("DeployRemoteWorker: OS keyring unavailable for %s; token not stored (user consent required for file fallback)", workspaceID)
			keyringUnavailable = true
			tokenKey = "" // ref remains unset until user opts in via StoreCFTokenFileFallback
		} else {
			log.Printf("DeployRemoteWorker: keyring set for %s: %v (non-fatal, token not persisted)", workspaceID, err)
			tokenKey = ""
		}
	}

	// Generate a 256-bit conductor API key and store it both in CF Secrets
	// and in the local key store so the conductor can authenticate requests.
	apiKey, err := randomKey256()
	if err != nil {
		return RemoteDeployResult{}, fmt.Errorf("generate conductor API key: %w", err)
	}
	if err := remoteworker.UpsertSecret(accountID, cfToken, deploy.WorkerName, "CONDUCTOR_API_KEY", apiKey); err != nil {
		return RemoteDeployResult{}, fmt.Errorf("store conductor API key secret: %w", err)
	}
	warn, err := remoteworker.SetKey(workspaceID, apiKey)
	if err != nil {
		return RemoteDeployResult{}, fmt.Errorf("store conductor API key locally: %w", err)
	}
	if warn != "" {
		a.emitToast("warning", "Remote Workspace", warn, nil)
	}

	// Update workspace RemoteConfig.
	rc := &types.RemoteConfig{
		CFAccountID:         accountID,
		CFWorkerName:        deploy.WorkerName,
		CFWorkerEndpointURL: deploy.CFWorkerEndpointURL,
		CFDeploymentVersion: deploy.DeploymentVersion,
		SecretRefs: types.RemoteSecretRefs{
			GitHubPATRef:       "GITHUB_PAT",
			CFAPITokenRef:      tokenKey,
			ConductorAPIKeyRef: "CONDUCTOR_API_KEY",
		},
	}
	ws.ExecutionTarget = types.ExecutionTargetRemote
	ws.RemoteConfig = rc
	if err := a.wsReg.Update(ws); err != nil {
		return RemoteDeployResult{}, fmt.Errorf("update workspace: %w", err)
	}

	return RemoteDeployResult{
		WorkerName:          deploy.WorkerName,
		CFWorkerEndpointURL: deploy.CFWorkerEndpointURL,
		DeploymentVersion:   deploy.DeploymentVersion,
		KeyringUnavailable:  keyringUnavailable,
	}, nil
}

// --- CF token helpers (issue #178) ---

// cfTokenForWorkspace retrieves the stored CF API token for a remote workspace.
// It reads the CFAPITokenRef from the workspace metadata and delegates to the
// appropriate backend (keyring or file fallback). Returns an error if no token
// is stored or the lookup fails.
func (a *App) cfTokenForWorkspace(wsID string) (string, error) {
	ws, ok := a.wsReg.Get(wsID)
	if !ok {
		return "", fmt.Errorf("workspace %s not found", wsID)
	}
	if ws.RemoteConfig == nil {
		return "", fmt.Errorf("workspace %s has no remote config", wsID)
	}
	ref := ws.RemoteConfig.SecretRefs.CFAPITokenRef
	if ref == "" {
		return "", fmt.Errorf("workspace %s has no stored CF token ref", wsID)
	}
	tok, err := secretstore.GetFromRef(ref, secretstore.DefaultSecretsDir())
	if err != nil {
		return "", fmt.Errorf("retrieve CF token for %s: %w", wsID, err)
	}
	return tok, nil
}

// StoreCFTokenFileFallback stores the CF API token for a workspace using the
// file-based fallback (~/.config/PrismConductor/secrets/<wsID>.key, 0600 perms).
// This is the explicit opt-in path shown to the user when the OS keyring is
// unavailable (headless Linux). The user must consent before this is called.
func (a *App) StoreCFTokenFileFallback(workspaceID, cfToken string) error {
	if a.wsReg == nil {
		return fmt.Errorf("workspace registry unavailable")
	}
	ws, ok := a.wsReg.Get(workspaceID)
	if !ok {
		return fmt.Errorf("workspace %s not found", workspaceID)
	}
	if ws.RemoteConfig == nil {
		return fmt.Errorf("workspace %s has no remote config", workspaceID)
	}

	dir := secretstore.DefaultSecretsDir()
	fs := secretstore.NewFileStore(dir)
	key := secretstore.CFTokenKey(workspaceID)
	if err := fs.Set(key, cfToken); err != nil {
		return fmt.Errorf("write file secret: %w", err)
	}
	filePath := fs.FilePath(key)
	log.Printf("StoreCFTokenFileFallback: token for %s stored at %s (user-consented file fallback)", workspaceID, filePath)

	ws.RemoteConfig.SecretRefs.CFAPITokenRef = secretstore.FileFallbackPrefix + filePath
	if err := a.wsReg.Update(ws); err != nil {
		return fmt.Errorf("update workspace: %w", err)
	}
	return nil
}

// RotateRemoteWorkerKey generates a fresh 256-bit conductor API key, stores it
// as a CF Secret on the worker, and updates the local key store. The old key
// becomes invalid immediately. A fresh cfToken must be provided because CF
// tokens are never stored locally.
func (a *App) RotateRemoteWorkerKey(workspaceID, cfToken string) error {
	if a.wsReg == nil {
		return fmt.Errorf("workspace registry unavailable")
	}
	ws, ok := a.wsReg.Get(workspaceID)
	if !ok {
		return fmt.Errorf("workspace %s not found", workspaceID)
	}
	rc := ws.RemoteConfig
	if rc == nil || rc.CFAccountID == "" || rc.CFWorkerName == "" {
		return fmt.Errorf("workspace %s is not a deployed remote workspace", workspaceID)
	}
	newKey, err := randomKey256()
	if err != nil {
		return fmt.Errorf("generate key: %w", err)
	}
	if err := remoteworker.UpsertSecret(rc.CFAccountID, cfToken, rc.CFWorkerName, "CONDUCTOR_API_KEY", newKey); err != nil {
		return fmt.Errorf("update CF secret: %w", err)
	}
	warn, err := remoteworker.SetKey(workspaceID, newKey)
	if err != nil {
		return fmt.Errorf("update local key: %w", err)
	}
	if warn != "" {
		a.emitToast("warning", "Remote Workspace", warn, nil)
	}
	return nil
}

// ReplaceCFToken rotates the stored CF API token for a workspace. It overwrites
// the existing keyring (or file fallback) entry so subsequent CF operations use
// the new token without requiring re-deploy.
func (a *App) ReplaceCFToken(workspaceID, newToken string) error {
	if a.wsReg == nil {
		return fmt.Errorf("workspace registry unavailable")
	}
	ws, ok := a.wsReg.Get(workspaceID)
	if !ok {
		return fmt.Errorf("workspace %s not found", workspaceID)
	}
	if ws.RemoteConfig == nil {
		return fmt.Errorf("workspace %s has no remote config", workspaceID)
	}
	ref := ws.RemoteConfig.SecretRefs.CFAPITokenRef

	if ref == "" {
		// No existing ref — attempt to store in keyring; fall through to file
		// fallback error so the caller can surface the consent banner.
		key := secretstore.CFTokenKey(workspaceID)
		if err := a.secretStore.Set(key, newToken); err != nil {
			if errors.Is(err, secretstore.ErrKeyringUnavailable) {
				return secretstore.ErrKeyringUnavailable
			}
			return fmt.Errorf("keyring set: %w", err)
		}
		ws.RemoteConfig.SecretRefs.CFAPITokenRef = key
		return a.wsReg.Update(ws)
	}

	// Overwrite existing ref location (keyring or file).
	if err := secretstore.DeleteFromRef(ref); err != nil {
		log.Printf("ReplaceCFToken: delete old ref %q: %v (continuing)", ref, err)
	}

	key := secretstore.CFTokenKey(workspaceID)
	if err := a.secretStore.Set(key, newToken); err != nil {
		if errors.Is(err, secretstore.ErrKeyringUnavailable) {
			return secretstore.ErrKeyringUnavailable
		}
		return fmt.Errorf("keyring set: %w", err)
	}
	ws.RemoteConfig.SecretRefs.CFAPITokenRef = key
	return a.wsReg.Update(ws)
}

// IsKeyringAvailable reports whether the OS-native keyring is reachable. The
// frontend uses this to decide whether to show the file-fallback consent banner.
func (a *App) IsKeyringAvailable() bool {
	const probe = "prismconductor.__keyring_probe__"
	if err := a.secretStore.Set(probe, "1"); err != nil {
		return false
	}
	_ = a.secretStore.Delete(probe)
	return true
}

// randomKey256 returns a cryptographically random 256-bit key encoded as
// base64url (no padding). Safe to use as a bearer token.
func randomKey256() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

// --- Goals ---

// ListGoals returns all goals (active first, then backlog, then achieved/abandoned).
func (a *App) ListGoals() ([]types.Goal, error) {
	if a.store == nil {
		return nil, fmt.Errorf("store unavailable")
	}
	return a.store.ListGoals()
}

// SaveGoal upserts a goal. Use this for Create + Edit; the frontend allocates the ID
// for new goals via uuid.
func (a *App) SaveGoal(g types.Goal) error {
	if a.store == nil {
		return fmt.Errorf("store unavailable")
	}
	if g.ID == "" {
		g.ID = uuid.NewString()
	}
	if g.CreatedAt.IsZero() {
		g.CreatedAt = time.Now()
	}
	if g.Status == "" {
		g.Status = types.GoalBacklog
	}
	if err := a.store.SaveGoal(g); err != nil {
		return err
	}
	a.bus.Publish(eventbus.EvtGoalUpdated, g.ID)
	return nil
}

// DeleteGoal removes a goal.
func (a *App) DeleteGoal(id string) error {
	if a.store == nil {
		return fmt.Errorf("store unavailable")
	}
	return a.store.DeleteGoal(id)
}

// ActivateGoal sets a goal to active, demoting any currently-active goal to backlog (§15.4).
func (a *App) ActivateGoal(id string) error {
	if a.store == nil {
		return fmt.Errorf("store unavailable")
	}
	if err := a.store.SetGoalActive(id); err != nil {
		return err
	}
	a.bus.Publish(eventbus.EvtGoalActivated, id)
	return nil
}

// --- Issues / Board ---

// ListIssues returns all non-archived issues, optionally filtered by workspace ID.
// Empty workspaceID = all workspaces. Archived rows (#34) are excluded; use
// ListArchivedIssues for the drawer.
func (a *App) ListIssues(workspaceID string) ([]types.Issue, error) {
	if a.store == nil {
		return nil, fmt.Errorf("store unavailable")
	}
	return a.store.ListIssues(workspaceID)
}

// FetchIssueDetail fetches fresh GitHub metadata for a single issue and caches
// the result for 60 seconds. Falls back to the locally mirrored issue on error.
func (a *App) FetchIssueDetail(workspaceID string, issueNumber int) (*types.Issue, error) {
	key := fmt.Sprintf("%s#%d", workspaceID, issueNumber)
	if v, ok := a.issueDetailCache.Load(key); ok {
		entry := v.(issueDetailEntry)
		if time.Now().Before(entry.expiresAt) {
			iss := entry.issue
			return &iss, nil
		}
	}
	if a.gh == nil || a.wsReg == nil {
		if iss, ok := a.findIssue(workspaceID, issueNumber); ok {
			return &iss, nil
		}
		return nil, fmt.Errorf("github client unavailable")
	}
	ws, ok := a.wsReg.Get(workspaceID)
	if !ok {
		return nil, fmt.Errorf("workspace %q not found", workspaceID)
	}
	iss, err := a.gh.FetchIssueDetail(a.ctx, ws, issueNumber)
	if err != nil {
		if local, ok := a.findIssue(workspaceID, issueNumber); ok {
			return &local, nil
		}
		return nil, err
	}
	// Preserve column/plan/session fields from local mirror.
	if local, ok := a.findIssue(workspaceID, issueNumber); ok {
		iss.Column = local.Column
		iss.Plan = local.Plan
		iss.SessionID = local.SessionID
		iss.GoalID = local.GoalID
		iss.Priority = local.Priority
		iss.Dependencies = local.Dependencies
		iss.DepRationale = local.DepRationale
		iss.PRNumber = local.PRNumber
		iss.PRURL = local.PRURL
	}
	a.issueDetailCache.Store(key, issueDetailEntry{issue: *iss, expiresAt: time.Now().Add(60 * time.Second)})
	return iss, nil
}

// ListArchivedIssues returns archived rows for the drawer (#34). Empty
// workspaceID returns archived rows across every workspace.
func (a *App) ListArchivedIssues(workspaceID string) ([]types.Issue, error) {
	if a.store == nil {
		return nil, fmt.Errorf("store unavailable")
	}
	return a.store.ListArchivedIssues(workspaceID)
}

// ArchiveDone flags every DONE row in the workspace as archived (#34). Returns
// the count archived. Empty workspaceID archives across every workspace
// (matches the All switcher case). Publishes EvtIssuesArchived when n > 0 so
// the live list and the drawer's (N) badge both refresh.
func (a *App) ArchiveDone(workspaceID string) (int, error) {
	if a.store == nil {
		return 0, fmt.Errorf("store unavailable")
	}
	n, err := a.store.ArchiveDone(workspaceID)
	if err != nil {
		return 0, err
	}
	if n > 0 && a.bus != nil {
		a.bus.Publish(eventbus.EvtIssuesArchived, map[string]any{
			"workspace_id": workspaceID,
			"count":        n,
		})
	}
	return n, nil
}

// runAutoArchiveAll runs auto-archive for every workspace that has it enabled.
// Publishes EvtIssuesArchived when any card is archived.
func (a *App) runAutoArchiveAll() {
	if a.store == nil || a.wsReg == nil {
		return
	}
	total := 0
	for _, ws := range a.wsReg.List() {
		n, err := archiver.RunAutoArchive(a.ctx, a.store, ws)
		if err != nil {
			log.Printf("auto-archive workspace %q: %v", ws.ID, err)
			continue
		}
		total += n
	}
	if total > 0 && a.bus != nil {
		a.bus.Publish(eventbus.EvtIssuesArchived, map[string]any{
			"workspace_id": "",
			"count":        total,
		})
	}
}

// RunAutoArchiveNow immediately runs auto-archive for the given workspace and
// returns the count of newly archived cards. Exposed to the frontend so the
// Settings panel can trigger a manual run.
func (a *App) RunAutoArchiveNow(workspaceID string) (int, error) {
	if a.store == nil {
		return 0, fmt.Errorf("store unavailable")
	}
	if a.wsReg == nil {
		return 0, fmt.Errorf("workspace registry unavailable")
	}
	ws, ok := a.wsReg.Get(workspaceID)
	if !ok {
		return 0, fmt.Errorf("workspace %q not found", workspaceID)
	}
	n, err := archiver.RunAutoArchive(a.ctx, a.store, ws)
	if err != nil {
		return 0, err
	}
	if n > 0 && a.bus != nil {
		a.bus.Publish(eventbus.EvtIssuesArchived, map[string]any{
			"workspace_id": workspaceID,
			"count":        n,
		})
	}
	return n, nil
}

// UnarchiveIssue clears archived_at for a single row (#34) and publishes
// EvtIssuesArchived so the live list and drawer both refresh.
func (a *App) UnarchiveIssue(workspaceID string, number int) error {
	if a.store == nil {
		return fmt.Errorf("store unavailable")
	}
	if err := a.store.UnarchiveIssue(workspaceID, number); err != nil {
		return err
	}
	if a.bus != nil {
		a.bus.Publish(eventbus.EvtIssuesArchived, map[string]any{
			"workspace_id": workspaceID,
			"number":       number,
			"count":        -1,
		})
	}
	return nil
}

// UnarchiveAll clears archived_at across the workspace (#34). Empty
// workspaceID restores everything across every workspace.
func (a *App) UnarchiveAll(workspaceID string) error {
	if a.store == nil {
		return fmt.Errorf("store unavailable")
	}
	if err := a.store.UnarchiveAll(workspaceID); err != nil {
		return err
	}
	if a.bus != nil {
		a.bus.Publish(eventbus.EvtIssuesArchived, map[string]any{
			"workspace_id": workspaceID,
			"count":        -1,
		})
	}
	return nil
}

// MoveIssueColumn moves an issue card between columns. Emits EvtCardMovedManually
// per §15.5 (orchestrator-driven moves bypass this method by writing the column
// directly). When the target is PLAN, also spawns the plan worker — that's
// the natural meaning of dragging a card into the PLAN column.
func (a *App) MoveIssueColumn(workspaceID string, number int, column string) error {
	log.Printf("MoveIssueColumn called: ws=%s #%d → %s", workspaceID, number, column)
	if a.store == nil {
		log.Printf("MoveIssueColumn: store unavailable")
		return fmt.Errorf("store unavailable")
	}
	if err := a.store.MoveIssueColumn(workspaceID, number, types.BoardColumn(column)); err != nil {
		log.Printf("MoveIssueColumn store error: %v", err)
		return err
	}
	a.bus.Publish(eventbus.EvtCardMovedManually, map[string]any{
		"workspace_id": workspaceID,
		"number":       number,
		"column":       column,
	})

	// Auto-acknowledge the most recent blocked/failed session when the user
	// moves the card to TODO or PLAN — dragging it back is the explicit signal
	// that they want to start over (issue #88).
	if types.BoardColumn(column) == types.ColTodo || types.BoardColumn(column) == types.ColPlan {
		if sess, err := a.store.AcknowledgeLatestFailure(workspaceID, number); err != nil {
			log.Printf("MoveIssueColumn: acknowledge failure for #%d: %v", number, err)
		} else if sess != nil {
			wruntime.EventsEmit(a.ctx, "session.state", *sess)
			log.Printf("MoveIssueColumn: acknowledged blocked session %s for #%d on move to %s", sess.ID[:8], number, column)
		}
	}

	if types.BoardColumn(column) == types.ColPlan && a.wsReg != nil && a.mgr != nil {
		// Auto-spawn rules:
		//   - Skip if a plan worker is already in flight (avoid double-spawn).
		//   - Skip if a plan already exists for this issue. Drag-to-PLAN with a
		//     prior plan is treated as "I'm just shuffling cards" — the user
		//     gets a Re-plan button on the card to force a new revision
		//     explicitly.
		//   - Otherwise: this is the first plan, spawn immediately.
		if a.hasActivePlanSession(workspaceID, number) {
			log.Printf("drag-to-PLAN: #%d already has an active plan session, skipping spawn", number)
		} else if a.hasExistingPlan(workspaceID, number) {
			log.Printf("drag-to-PLAN: #%d already has a plan; user must click Re-plan to force a new revision", number)
		} else if a.hasRecentFailedPlan(workspaceID, number) {
			// The most recent plan run for this issue blew up within the last
			// 30 minutes (BLOCKED or FAILED, no plan produced). Don't silently
			// auto-respawn — that's how the user racks up surprise paid runs
			// in a tight loop. Surface a toast so the user knows why nothing
			// happened and force them to click Re-plan if they actually want
			// to retry. Older failures don't gate; the window slides.
			log.Printf("drag-to-PLAN: #%d has a recent failed plan session; click Re-plan to retry explicitly", number)
			a.emitToast("warning", toastWorkspaceName(a.wsReg, workspaceID),
				fmt.Sprintf("#%d had a recent failed plan; click Re-plan on the card to retry (skipped auto-spawn to avoid a surprise paid run)", number),
				map[string]any{
					"workspace_id": workspaceID,
					"issue_number": number,
					"action":       "focus_card",
				})
		} else {
			ws, ok := a.wsReg.Get(workspaceID)
			if !ok {
				log.Printf("drag-to-PLAN: workspace %q not found", workspaceID)
			} else {
				log.Printf("drag-to-PLAN: spawning plan worker for %s#%d (no existing plan)", workspaceID, number)
				pool, ok := a.acquirePlanPool(ws)
				if !ok {
					// No plan slot — persist intent so the orchestrator's drain
					// loop spawns the worker as soon as a slot frees, and flip
					// WaitingForPool so the card shows the pink "waiting"
					// decoration instead of sitting idle in the PLAN column
					// with no glow and no indicator. Without this, dragging a
					// 2nd card into PLAN while the planner pool is saturated
					// silently no-ops — looks like the system "forgot" the
					// card.
					log.Printf("auto-spawn plan #%d: no plan pool available — enqueuing", number)
					_ = a.store.EnqueuePendingPool(workspaceID, number, types.RolePlan, "drag_to_plan")
					_ = a.store.SetIssueWaitingForPool(workspaceID, number, true)
					a.bus.Publish(eventbus.EvtPendingPoolEnqueued, eventbus.PendingPoolChange{
						WorkspaceID: workspaceID,
						IssueNumber: number,
						Role:        string(types.RolePlan),
					})
				} else {
					sess, err := a.mgr.SpawnPlan(ws, types.Issue{Number: number, WorkspaceID: workspaceID}, pool)
					if err != nil {
						a.poolReg.ReleaseByPool(pool.ID)
						log.Printf("auto-spawn plan #%d FAILED: %v", number, err)
					} else {
						log.Printf("drag-to-PLAN: spawn ok for #%d, session=%s pid=%d pool=%s", number, sess.ID[:8], sess.PID, pool.ID)
					}
				}
			}
		}
	}
	return nil
}

// ClearIssueFailure acknowledges the most recent blocked/failed session for
// the given issue (issue #88). The session row is preserved for audit;
// acknowledged_at is set and a session.state event is emitted so the frontend
// immediately removes the blocked overlay without needing an app restart.
func (a *App) ClearIssueFailure(workspaceID string, issueNumber int) error {
	if a.store == nil {
		return fmt.Errorf("store unavailable")
	}
	if a.wsReg != nil {
		if _, ok := a.wsReg.Get(workspaceID); !ok {
			return fmt.Errorf("unknown workspace %q", workspaceID)
		}
	}
	sess, err := a.store.AcknowledgeLatestFailure(workspaceID, issueNumber)
	if err != nil {
		return err
	}
	// Clear the persisted failure_reason so the card no longer shows the
	// blocked tooltip after the user acknowledges the failure (#194).
	_ = a.store.SetIssueFailureReason(workspaceID, issueNumber, "")
	// Emit orphan PR cleared so the assembler removes the recovery button.
	if a.bus != nil {
		a.bus.Publish(eventbus.EvtOrphanPRCleared, eventbus.OrphanPRCleared{
			WorkspaceID: workspaceID,
			IssueNumber: issueNumber,
		})
	}
	if sess != nil {
		wruntime.EventsEmit(a.ctx, "session.state", *sess)
		// Publish to the in-process bus too so the IssueView assembler
		// reassembles and emits a fresh bus.issue_view_updated. Without
		// this, the assembler keeps surfacing the now-acknowledged session
		// as last_failure, the card's red overlay never clears, and the
		// "Clear Failure" button looks broken even though the DB row is
		// correctly acknowledged.
		if a.bus != nil {
			a.bus.Publish(eventbus.EvtSessionStateChanged, eventbus.SessionStateChanged{
				WorkspaceID: sess.WorkspaceID,
				IssueNumber: sess.IssueNumber,
				SessionID:   sess.ID,
			})
		}
	} else if a.assembler != nil {
		// No row matched the acknowledge query — could be the issue's
		// frontend view is stale and still shows lastFailure even though
		// nothing in DB is unacknowledged. Force a reassemble so the
		// frontend's IssueView gets rebuilt from scratch and the button
		// at least makes the UI converge with backend truth.
		a.assembler.Reassemble(workspaceID, issueNumber)
	}
	return nil
}

// OpenOrphanPR creates a draft PR for a pushed branch from a failed execute
// session that never opened a PR. Transitions the card to REVIEW on success
// (#194). Returns an error if no orphan session exists or the API call fails.
func (a *App) OpenOrphanPR(workspaceID string, issueNumber int) error {
	if a.store == nil {
		return fmt.Errorf("store unavailable")
	}
	ws, ok := a.wsReg.Get(workspaceID)
	if !ok {
		return fmt.Errorf("unknown workspace %q", workspaceID)
	}
	sess, err := a.store.MostRecentFailedExecuteSession(workspaceID, issueNumber)
	if err != nil {
		return fmt.Errorf("lookup failed execute session: %w", err)
	}
	if sess == nil || sess.Branch == "" {
		return fmt.Errorf("no failed execute session with a pushed branch for issue #%d", issueNumber)
	}
	if a.gh == nil {
		return fmt.Errorf("github client not configured")
	}
	iss, err := a.store.LoadIssue(workspaceID, issueNumber)
	if err != nil {
		return fmt.Errorf("load issue: %w", err)
	}
	prNum, prURL, err := a.gh.CreateDraftPR(a.ctx, ws, sess.Branch, iss.Title, issueNumber)
	if err != nil {
		return fmt.Errorf("create draft PR: %w", err)
	}
	if err := a.store.MarkPROpened(workspaceID, issueNumber, prNum, prURL); err != nil {
		return fmt.Errorf("mark PR opened: %w", err)
	}
	// Clear orphan state and emit PR-opened so the assembler moves the card.
	_ = a.store.SetIssueFailureReason(workspaceID, issueNumber, "")
	if a.bus != nil {
		a.bus.Publish(eventbus.EvtPROpened, map[string]any{
			"workspace_id": workspaceID,
			"issue_number": issueNumber,
			"pr_number":    prNum,
			"pr_url":       prURL,
		})
	}
	wruntime.EventsEmit(a.ctx, "pr.opened", map[string]any{
		"workspace_id": workspaceID,
		"issue_number": issueNumber,
		"pr_number":    prNum,
		"pr_url":       prURL,
	})
	return nil
}

// hasExistingPlan returns true if any plan revision has been written for this
// issue. Used to gate auto-replan on drag-to-PLAN.
func (a *App) hasExistingPlan(workspaceID string, number int) bool {
	if a.store == nil {
		return false
	}
	p, err := a.store.LatestPlan(workspaceID, number)
	return err == nil && p != nil
}

// Replan force-spawns a fresh plan worker for an issue regardless of existing
// plans or active sessions. Frontend calls this from the "Re-plan" button on
// a card that already has a plan.
func (a *App) Replan(workspaceID string, number int) error {
	if a.wsReg == nil || a.mgr == nil {
		return fmt.Errorf("registry/manager unavailable")
	}
	ws, ok := a.wsReg.Get(workspaceID)
	if !ok {
		return fmt.Errorf("unknown workspace %q", workspaceID)
	}
	if a.hasActivePlanSession(workspaceID, number) {
		return fmt.Errorf("plan worker already in flight for #%d", number)
	}
	log.Printf("Replan: spawning plan worker for %s#%d", workspaceID, number)
	pool, ok := a.acquirePlanPool(ws)
	if !ok {
		return fmt.Errorf("no plan pool available")
	}
	sess, err := a.mgr.SpawnPlan(ws, types.Issue{Number: number, WorkspaceID: workspaceID}, pool)
	if err != nil {
		a.poolReg.ReleaseByPool(pool.ID)
		log.Printf("Replan #%d FAILED: %v", number, err)
		return err
	}
	log.Printf("Replan: spawn ok for #%d, session=%s pid=%d pool=%s", number, sess.ID[:8], sess.PID, pool.ID)
	return nil
}

// RecentLogs returns the in-memory log ring for the Settings → Logs panel.
func (a *App) RecentLogs() []logbuffer.Entry {
	if a.logs == nil {
		return nil
	}
	return a.logs.Snapshot()
}

// hasRecentFailedPlan returns true when the most recent plan-mode session
// for the issue ended in failed/blocked within the last 30 minutes. Used to
// block drag-to-PLAN auto-spawn from immediately re-billing the user after a
// fresh failure; old historical failures don't gate forever.
func (a *App) hasRecentFailedPlan(workspaceID string, number int) bool {
	if a.store == nil {
		return false
	}
	has, err := a.store.HasRecentFailedPlanSession(workspaceID, number, 30*time.Minute)
	if err != nil {
		log.Printf("hasRecentFailedPlan #%d: %v", number, err)
		return false
	}
	return has
}

// hasActivePlanSession returns true if a plan-mode worker is currently running
// (or waiting / blocked) for the given issue.
func (a *App) hasActivePlanSession(workspaceID string, number int) bool {
	if a.mgr == nil {
		return false
	}
	for _, s := range a.mgr.Snapshot() {
		if s.WorkspaceID != workspaceID || s.IssueNumber != number {
			continue
		}
		if s.Mode != types.ModePlan {
			continue
		}
		switch s.State {
		case types.StateRunning, types.StateWaitingForInput, types.StateBlocked:
			return true
		}
	}
	return false
}

// ReorderIssues persists a new ordering of issues within a single column.
// Pass numbers in their final left-to-right or top-to-bottom order.
func (a *App) ReorderIssues(workspaceID string, column string, ordered []int) error {
	if a.store == nil {
		return fmt.Errorf("store unavailable")
	}
	if err := a.store.ReorderIssues(workspaceID, types.BoardColumn(column), ordered); err != nil {
		return err
	}
	a.bus.Publish(eventbus.EvtCardMovedManually, map[string]any{
		"workspace_id": workspaceID,
		"column":       column,
		"ordered":      ordered,
	})
	return nil
}

// RemoveIssue deletes a local issue row. (GitHub-mirrored issues are restored
// on next poll; conductor-only test issues stay gone.)
func (a *App) RemoveIssue(workspaceID string, number int) error {
	if a.store == nil {
		return fmt.Errorf("store unavailable")
	}
	return a.store.RemoveIssue(workspaceID, number)
}

// RefreshIssuesNow asks the poller to fan out a fresh GitHub fetch right now.
func (a *App) RefreshIssuesNow() error {
	if a.poller == nil {
		return fmt.Errorf("github poller not available — is `gh` CLI authenticated?")
	}
	a.poller.PokeNow()
	return nil
}

// SetPollInterval persists the poll interval (seconds, min 30).
func (a *App) SetPollInterval(seconds int) error {
	if seconds < 30 {
		seconds = 30
	}
	if a.store == nil {
		return fmt.Errorf("store unavailable")
	}
	return a.store.SetSetting("poll_interval_seconds", strconv.Itoa(seconds))
}

// GetPollInterval returns the current interval in seconds (default 300).
func (a *App) GetPollInterval() int {
	if a.store == nil {
		return 300
	}
	v, _ := a.store.GetSetting("poll_interval_seconds")
	if v == "" {
		return 300
	}
	n, err := strconv.Atoi(v)
	if err != nil || n < 30 {
		return 300
	}
	return n
}

// --- Label filter (#110) ---

// LabelFilterState is the shape returned by GetLabelFilter and consumed by the frontend store.
type LabelFilterState struct {
	Labels []string `json:"labels"`
	Mode   string   `json:"mode"`
}

// GetLabelFilter returns the persisted label filter selection for a workspace.
func (a *App) GetLabelFilter(workspaceID string) (LabelFilterState, error) {
	if a.store == nil {
		return LabelFilterState{Labels: []string{}, Mode: "or"}, nil
	}
	labels, mode, err := a.store.GetLabelFilter(workspaceID)
	if err != nil {
		return LabelFilterState{Labels: []string{}, Mode: "or"}, err
	}
	return LabelFilterState{Labels: labels, Mode: mode}, nil
}

// SetLabelFilter persists the label filter selection for a workspace.
func (a *App) SetLabelFilter(workspaceID string, labels []string, mode string) error {
	if a.store == nil {
		return fmt.Errorf("store unavailable")
	}
	return a.store.SetLabelFilter(workspaceID, labels, mode)
}

// --- Labels (#14) ---

// ListLabels returns the cached label set for a workspace. The poller fans
// these in on every 5-min tick and after every label CRUD call.
func (a *App) ListLabels(workspaceID string) ([]types.Label, error) {
	if a.store == nil {
		return nil, fmt.Errorf("store unavailable")
	}
	return a.store.ListLabels(workspaceID)
}

// refreshLabels pulls fresh labels from GitHub for one workspace, mirrors them
// locally, and publishes EvtLabelsUpdated. The single chokepoint for cache
// updates so a future webhook listener plugs in cleanly.
func (a *App) refreshLabels(workspaceID string) error {
	if a.gh == nil || a.wsReg == nil || a.store == nil {
		return fmt.Errorf("github client / registry / store unavailable")
	}
	ws, ok := a.wsReg.Get(workspaceID)
	if !ok {
		return fmt.Errorf("unknown workspace %q", workspaceID)
	}
	labels, err := a.gh.ListLabels(a.ctx, ws)
	if err != nil {
		return err
	}
	if err := a.store.SaveLabels(workspaceID, labels); err != nil {
		return err
	}
	a.bus.Publish(eventbus.EvtLabelsUpdated, map[string]any{"workspace_id": workspaceID})
	return nil
}

// CreateLabel creates a label on GitHub and refreshes the local cache.
func (a *App) CreateLabel(workspaceID string, label types.Label) (types.Label, error) {
	if a.gh == nil || a.wsReg == nil {
		return types.Label{}, fmt.Errorf("github unavailable")
	}
	ws, ok := a.wsReg.Get(workspaceID)
	if !ok {
		return types.Label{}, fmt.Errorf("unknown workspace %q", workspaceID)
	}
	out, err := a.gh.CreateLabel(a.ctx, ws, label)
	if err != nil {
		return types.Label{}, err
	}
	if err := a.refreshLabels(workspaceID); err != nil {
		log.Printf("refresh labels after create: %v", err)
	}
	return out, nil
}

// UpdateLabel edits a label on GitHub, refreshes the cache, and pokes the
// poller so issue rows reconcile their label arrays (Q4 = yes).
func (a *App) UpdateLabel(workspaceID, originalName string, label types.Label) (types.Label, error) {
	if a.gh == nil || a.wsReg == nil {
		return types.Label{}, fmt.Errorf("github unavailable")
	}
	ws, ok := a.wsReg.Get(workspaceID)
	if !ok {
		return types.Label{}, fmt.Errorf("unknown workspace %q", workspaceID)
	}
	out, err := a.gh.UpdateLabel(a.ctx, ws, originalName, label)
	if err != nil {
		return types.Label{}, err
	}
	if err := a.refreshLabels(workspaceID); err != nil {
		log.Printf("refresh labels after update: %v", err)
	}
	if a.poller != nil {
		a.poller.PokeNow()
	}
	return out, nil
}

// DeleteLabel removes a label on GitHub, refreshes the cache, and pokes the
// poller so issue rows that referenced it reconcile.
func (a *App) DeleteLabel(workspaceID, name string) error {
	if a.gh == nil || a.wsReg == nil {
		return fmt.Errorf("github unavailable")
	}
	ws, ok := a.wsReg.Get(workspaceID)
	if !ok {
		return fmt.Errorf("unknown workspace %q", workspaceID)
	}
	if err := a.gh.DeleteLabel(a.ctx, ws, name); err != nil {
		return err
	}
	if err := a.refreshLabels(workspaceID); err != nil {
		log.Printf("refresh labels after delete: %v", err)
	}
	if a.poller != nil {
		a.poller.PokeNow()
	}
	return nil
}

// SetIssueLabels replaces an issue's labels on GitHub. Updates the local row
// optimistically (Q2) so chips don't flicker, publishes
// EvtIssueLabelChanged, then pokes the poller to reconcile.
func (a *App) SetIssueLabels(workspaceID string, issueNumber int, names []string) error {
	if a.gh == nil || a.wsReg == nil || a.store == nil {
		return fmt.Errorf("github / registry / store unavailable")
	}
	ws, ok := a.wsReg.Get(workspaceID)
	if !ok {
		return fmt.Errorf("unknown workspace %q", workspaceID)
	}
	if err := a.gh.SetIssueLabels(a.ctx, ws, issueNumber, names); err != nil {
		return err
	}
	// Optimistic local mirror update so the UI flips without waiting for the
	// next poll. Find the existing row, swap the Labels slice, save back.
	if existing, err := a.store.ListIssues(workspaceID); err == nil {
		for _, iss := range existing {
			if iss.Number != issueNumber {
				continue
			}
			iss.Labels = append([]string(nil), names...)
			if _, err := a.store.SaveIssue(iss); err != nil {
				log.Printf("optimistic label save #%d: %v", issueNumber, err)
			}
			break
		}
	}
	a.bus.Publish(eventbus.EvtIssueLabelChanged, map[string]any{
		"workspace_id": workspaceID,
		"number":       issueNumber,
	})
	if a.poller != nil {
		a.poller.PokeNow()
	}
	return nil
}

// autoApplyPlanLabels intersects plan.SuggestedLabels with the workspace label
// cache, drops missing names (logged, never invented — issue #24 q3 C),
// reconciles the prior plan revision's axis label (q2 A), and pushes the
// union via SetIssueLabels (which fires EvtIssueLabelChanged — q4 A).
func (a *App) autoApplyPlanLabels(ws types.Workspace, issueNumber int, plan *types.Plan) {
	if a.store == nil {
		return
	}
	cache, err := a.store.ListLabels(ws.ID)
	if err != nil {
		log.Printf("auto-apply labels #%d: list cache: %v", issueNumber, err)
		return
	}
	inCache := make(map[string]struct{}, len(cache))
	for _, l := range cache {
		inCache[l.Name] = struct{}{}
	}

	var keep []string
	for _, name := range plan.SuggestedLabels {
		if _, ok := inCache[name]; ok {
			keep = append(keep, name)
		} else {
			log.Printf("auto-apply #%d: dropped suggestion %q (not in repo)", issueNumber, name)
		}
	}

	issue, ok := a.findIssue(ws.ID, issueNumber)
	if !ok {
		return
	}
	priorAxis := a.priorAxisLabel(ws.ID, issueNumber, plan.Revision)
	next := reconcileAutoLabels(issue.Labels, keep, priorAxis)
	if labelSetEqual(issue.Labels, next) {
		return
	}
	if err := a.SetIssueLabels(ws.ID, issueNumber, next); err != nil {
		log.Printf("auto-apply #%d: SetIssueLabels: %v", issueNumber, err)
	}
}

// priorAxisLabel returns the first label of the issue's previous plan revision
// (if any). Used to decide whether to drop the obsolete axis on re-plan.
func (a *App) priorAxisLabel(workspaceID string, issueNumber, revision int) string {
	if a.store == nil || revision <= 1 {
		return ""
	}
	prior, err := a.store.GetPlan(workspaceID, issueNumber, revision-1)
	if err != nil || len(prior.SuggestedLabels) == 0 {
		return ""
	}
	return prior.SuggestedLabels[0]
}

// reconcileAutoLabels implements issue #24 q2 A:
//   - start from current
//   - if priorAxis is on current AND not in keep, drop it
//   - union with keep
//   - preserve original ordering of current (minus dropped axis), then append new
func reconcileAutoLabels(current, keep []string, priorAxis string) []string {
	keepSet := make(map[string]struct{}, len(keep))
	for _, k := range keep {
		keepSet[k] = struct{}{}
	}
	have := make(map[string]struct{}, len(current))
	out := make([]string, 0, len(current)+len(keep))
	for _, c := range current {
		if priorAxis != "" && c == priorAxis {
			if _, stillSuggested := keepSet[c]; !stillSuggested {
				continue
			}
		}
		if _, dup := have[c]; dup {
			continue
		}
		have[c] = struct{}{}
		out = append(out, c)
	}
	for _, k := range keep {
		if _, dup := have[k]; dup {
			continue
		}
		have[k] = struct{}{}
		out = append(out, k)
	}
	return out
}

func labelSetEqual(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	seen := make(map[string]struct{}, len(a))
	for _, x := range a {
		seen[x] = struct{}{}
	}
	for _, x := range b {
		if _, ok := seen[x]; !ok {
			return false
		}
	}
	return true
}

func (a *App) findIssue(workspaceID string, issueNumber int) (types.Issue, bool) {
	if a.store == nil {
		return types.Issue{}, false
	}
	rows, err := a.store.ListIssues(workspaceID)
	if err != nil {
		return types.Issue{}, false
	}
	for _, iss := range rows {
		if iss.Number == issueNumber {
			return iss, true
		}
	}
	return types.Issue{}, false
}

// AddManualIssue lets the user create a fake issue card in any column. Used to
// drive the board UI before #2 lands the real GitHub fetch loop.
func (a *App) AddManualIssue(workspaceID string, number int, title, body string, labels []string, column string) (*types.Issue, error) {
	if a.store == nil {
		return nil, fmt.Errorf("store unavailable")
	}
	if column == "" {
		column = string(types.ColTodo)
	}
	iss := types.Issue{
		Number:      number,
		WorkspaceID: workspaceID,
		Title:       title,
		Body:        body,
		Labels:      labels,
		State:       "open",
		Column:      types.BoardColumn(column),
	}
	if _, err := a.store.SaveIssue(iss); err != nil {
		return nil, err
	}
	a.bus.Publish(eventbus.EvtIssueAdded, map[string]any{
		"workspace_id": workspaceID,
		"number":       number,
	})
	return &iss, nil
}

// --- Pools (§6.6, issue #27) ---

// ProviderInfo is the read-only descriptor a UI uses to render the
// PoolEditModal's provider dropdown.
type ProviderInfo struct {
	Kind            types.Provider `json:"kind"`
	DisplayName     string         `json:"display_name"`
	DefaultEndpoint string         `json:"default_endpoint"`
	NeedsAPIKey     bool           `json:"needs_api_key"`
	CanSpawn        bool           `json:"can_spawn"`
}

// ListProviders returns one ProviderInfo per registered LLM driver.
func (a *App) ListProviders() []ProviderInfo {
	if a.providers == nil {
		return nil
	}
	all := a.providers.All()
	out := make([]ProviderInfo, 0, len(all))
	for _, p := range all {
		out = append(out, ProviderInfo{
			Kind:            p.Kind(),
			DisplayName:     p.DisplayName(),
			DefaultEndpoint: p.DefaultEndpoint(),
			NeedsAPIKey:     p.NeedsAPIKey(),
			CanSpawn:        p.CanSpawn(),
		})
	}
	return out
}

// ListPools returns per-pool snapshot rows for the UI.
func (a *App) ListPools() []workerpool.PoolStatus {
	if a.poolReg == nil {
		return nil
	}
	return a.poolReg.Snapshot()
}

// ResetPoolCounters zeros the in-memory active-slot counter on every pool.
// Used to recover from drift caused by orphaned sessions (e.g. conductor
// killed mid-session before tailAndParse could publish WorkerSlotFreed).
// Does not touch any actual running worker — only the counter accounting.
// Returns the number of pools whose counter was reset.
func (a *App) ResetPoolCounters() int {
	if a.poolReg == nil {
		return 0
	}
	n := a.poolReg.ResetAllActive()
	if a.bus != nil {
		a.bus.Publish(eventbus.EvtAgentCountChanged, "manual_reset")
	}
	log.Printf("ResetPoolCounters: zeroed active counters on %d pools", n)
	return n
}

// SavePool upserts a pool row and re-syncs the registry.
func (a *App) SavePool(p types.Pool) error {
	if a.store == nil {
		return fmt.Errorf("store unavailable")
	}
	if p.ID == "" {
		p.ID = uuid.NewString()
	}
	if p.Capacity < 0 {
		p.Capacity = 0
	}
	if p.Capacity > 10 {
		p.Capacity = 10
	}
	if p.CreatedAt.IsZero() {
		p.CreatedAt = time.Now()
	}
	if err := a.store.SavePool(p); err != nil {
		return err
	}
	rows, err := a.store.ListPools()
	if err != nil {
		return err
	}
	a.poolReg.Sync(rows)
	a.bus.Publish(eventbus.EvtAgentCountChanged, map[string]any{"pool_id": p.ID})
	return nil
}

// ErrPoolBusy signals that DeletePool refused because workers are still
// running on the pool (rev4 q2).
type ErrPoolBusy struct {
	ID     string `json:"id"`
	Active int    `json:"active"`
}

func (e ErrPoolBusy) Error() string {
	return fmt.Sprintf("pool busy: %d active worker(s)", e.Active)
}

// DeletePool refuses if any worker is still active on the pool, otherwise
// drops the row and re-syncs the registry.
func (a *App) DeletePool(id string) error {
	if a.store == nil || a.poolReg == nil {
		return fmt.Errorf("store/registry unavailable")
	}
	if active := a.poolReg.ActiveCount(id); active > 0 {
		return ErrPoolBusy{ID: id, Active: active}
	}
	if err := a.store.DeletePool(id); err != nil {
		return err
	}
	rows, err := a.store.ListPools()
	if err != nil {
		return err
	}
	a.poolReg.Sync(rows)
	a.bus.Publish(eventbus.EvtAgentCountChanged, map[string]any{"pool_id": id, "deleted": true})
	return nil
}

// ProbeProviderModels asks the named provider to list available models against
// the supplied endpoint + apiKey. The modal uses this both for the on-blur
// model dropdown and the explicit Test connection button.
func (a *App) ProbeProviderModels(provider types.Provider, endpoint, apiKey string) ([]string, error) {
	if a.providers == nil {
		return nil, fmt.Errorf("providers unavailable")
	}
	prov, ok := a.providers.Get(provider)
	if !ok {
		return nil, fmt.Errorf("unknown provider %q", provider)
	}
	pool := types.Pool{Provider: provider, Endpoint: endpoint, APIKey: apiKey}
	return prov.ListModels(a.ctx, pool)
}

// migrateClaudePoolFromLegacy seeds one Claude pool the first time the pools
// table is empty. Capacity comes from the legacy worker_pool_capacity setting
// (default 2). The legacy KV row is left readable for diagnostics; #39 is
// responsible for migrating the Ollama orchestrator-config row.
func (a *App) migrateClaudePoolFromLegacy() {
	if a.store == nil {
		return
	}
	capacity := 2
	if c, _ := a.store.GetSetting("worker_pool_capacity"); c != "" {
		if n, err := strconv.Atoi(c); err == nil && n > 0 {
			capacity = n
		}
	}
	model := "claude-opus-4-7"
	pool := types.Pool{
		ID:        uuid.NewString(),
		Name:      model,
		Provider:  types.ProviderClaude,
		Endpoint:  "",
		Model:     model,
		Capacity:  capacity,
		Enabled:   true,
		CreatedAt: time.Now(),
	}
	if err := a.store.SavePool(pool); err != nil {
		log.Printf("migrate claude pool: save: %v", err)
	}
}

// GetAutoPullPaused returns the live pause state from the orchestrator's
// atomic.Bool — no DB round-trip per call.
func (a *App) GetAutoPullPaused() bool {
	if a.orch == nil {
		return false
	}
	return a.orch.IsPaused()
}

// SetAutoPullPaused persists the toggle and updates the running orchestrator.
// On resume (paused=false), kicks autoPull so catch-up is immediate.
func (a *App) SetAutoPullPaused(paused bool) error {
	if a.orch == nil || a.store == nil {
		return fmt.Errorf("orchestrator/store unavailable")
	}
	a.orch.SetPaused(paused)
	val := "false"
	if paused {
		val = "true"
	}
	if err := a.store.SetSetting("auto_pull_paused", val); err != nil {
		return err
	}
	a.bus.Publish(eventbus.EvtAutoPullPausedChanged, map[string]any{"paused": paused})
	if !paused {
		a.orch.KickAutoPull("resumed")
	}
	return nil
}

// --- Skills (§15.7, issue #92) ---

// migrateWorkspaceSkillProfiles runs the one-time migration that synthesizes
// PerStage from legacy Mode/UseConductor* fields for every workspace.
func (a *App) migrateWorkspaceSkillProfiles() {
	if a.wsReg == nil {
		return
	}
	for _, ws := range a.wsReg.List() {
		if skills.MigrateSkillProfile(&ws.SkillProfile) {
			if err := a.wsReg.Update(ws); err != nil {
				log.Printf("skill profile migration for workspace %s: %v", ws.ID, err)
			}
		}
	}
}

// DiscoverWorkspaceSkills returns all available skills for the given workspace:
// bundled defaults plus any markdown files discovered in the repo.
func (a *App) DiscoverWorkspaceSkills(workspaceID string) ([]types.SkillRef, error) {
	if a.wsReg == nil {
		return nil, fmt.Errorf("workspace registry not ready")
	}
	ws, ok := a.wsReg.Get(workspaceID)
	if !ok {
		return nil, fmt.Errorf("workspace %q not found", workspaceID)
	}
	return skills.Discover(ws.RepoPath, bundle.FS)
}

// --- Bundled skills (§15.7) ---

// BundledSkill is the read-only view of an extracted skill on disk.
type BundledSkill struct {
	Name string `json:"name"`
	Path string `json:"path"`
	Body string `json:"body"`
}

// ListBundledSkills returns the four universal skills extracted on first run.
// The frontend uses this for the read-only viewer + "Open in editor" link.
func (a *App) ListBundledSkills() ([]BundledSkill, error) {
	dir := filepath.Join(a.cfgDir, "skills")
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	var out []BundledSkill
	for _, e := range entries {
		if e.IsDir() || filepath.Ext(e.Name()) != ".md" {
			continue
		}
		path := filepath.Join(dir, e.Name())
		b, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		out = append(out, BundledSkill{
			Name: e.Name()[:len(e.Name())-3],
			Path: path,
			Body: string(b),
		})
	}
	return out, nil
}

// OpenBundledSkill opens the on-disk skill markdown in the user's default editor.
func (a *App) OpenBundledSkill(name string) error {
	path := filepath.Join(a.cfgDir, "skills", name+".md")
	if _, err := os.Stat(path); err != nil {
		return err
	}
	wruntime.BrowserOpenURL(a.ctx, "file://"+path)
	return nil
}

// --- Pipeline (§issue #146) ---

// ListAvailableSkills returns all skills available for use as pipeline steps:
// bundled skills plus any discovered in the workspace's repo directories.
func (a *App) ListAvailableSkills(workspaceID string) ([]types.SkillRef, error) {
	if a.wsReg == nil {
		return nil, fmt.Errorf("workspace registry not ready")
	}
	ws, ok := a.wsReg.Get(workspaceID)
	if !ok {
		return nil, fmt.Errorf("workspace %q not found", workspaceID)
	}
	return skills.ScanForPipeline(ws.RepoPath, bundle.FS)
}

// ValidatePipeline checks the pipeline for structural errors: duplicate IDs,
// unknown step references, and uncapped cycles (q3: no).
func (a *App) ValidatePipeline(p types.WorkspacePipeline) error {
	return pipeline.Validate(&p)
}

// SaveWorkspacePipeline persists the pipeline configuration for a workspace.
// The pipeline version is incremented on each save so in-flight cards that
// were stamped with an older version continue on their saved pipeline.
func (a *App) SaveWorkspacePipeline(workspaceID string, p types.WorkspacePipeline) error {
	if a.wsReg == nil {
		return fmt.Errorf("workspace registry not ready")
	}
	if err := pipeline.Validate(&p); err != nil {
		return fmt.Errorf("invalid pipeline: %w", err)
	}
	ws, ok := a.wsReg.Get(workspaceID)
	if !ok {
		return fmt.Errorf("workspace %q not found", workspaceID)
	}
	if ws.Pipeline != nil {
		p.Version = ws.Pipeline.Version + 1
	} else {
		p.Version = 1
	}
	ws.Pipeline = &p
	return a.wsReg.Update(ws)
}

// --- Plans + Q&A loop (§9, §10) ---

// handlePlanReady reads + persists the plan file when the worker emits the
// "Plan written" sentinel.
func (a *App) handlePlanReady(sess types.Session, relPath string) {
	if a.wsReg == nil || a.store == nil {
		return
	}
	ws, ok := a.wsReg.Get(sess.WorkspaceID)
	if !ok {
		return
	}
	abs := filepath.Join(ws.RepoPath, relPath)
	plan, err := planio.ReadPlan(abs)
	if err != nil {
		log.Printf("plan ingest failed: %v\n", err)
		a.markSessionBlocked(sess, fmt.Sprintf("plan ingest failed: %v", err))
		return
	}

	// Phase 1 (issue #197): reject the plan if the planner never read the issue.
	// We check the live transcript (not the plan file) because the planner writes
	// the plan *after* reading the issue; any evidence in the transcript is proof.
	if sess.Transcript != "" {
		read, scanErr := session.ScanForIssueRead(sess.Transcript, sess.IssueNumber)
		if scanErr != nil {
			log.Printf("transcript scan error (issue #%d): %v — accepting plan cautiously", sess.IssueNumber, scanErr)
		} else if !read {
			reason := fmt.Sprintf("planner did not read the issue: no gh issue view, pre-fetched payload read, or API fetch found in transcript (issue #197)")
			log.Printf("plan rejected: %s", reason)
			a.markSessionBlocked(sess, reason)
			return
		}
	}

	// Phase 2 (issue #197): advisory token-overlap check. A score below the
	// threshold is a warning, not a hard block — the transcript scan above is
	// the gate; this surfaces relevance problems that survive it.
	if iss, loadErr := a.store.LoadIssue(sess.WorkspaceID, sess.IssueNumber); loadErr == nil {
		issueText := iss.Title + " " + iss.Body
		score := planio.TokenOverlapScore(issueText, plan.PlanMarkdown)
		if score < planio.DefaultOverlapThreshold {
			log.Printf("plan relevance warning (issue #%d rev %d): token overlap %.2f < %.2f — plan may be off-topic",
				sess.IssueNumber, plan.Revision, score, planio.DefaultOverlapThreshold)
		}
	}

	plan.WorkspaceID = sess.WorkspaceID
	plan.IssueNumber = sess.IssueNumber
	if err := a.store.SavePlan(*plan); err != nil {
		log.Printf("plan save failed: %v\n", err)
		a.markSessionBlocked(sess, fmt.Sprintf("plan save failed: %v", err))
		return
	}
	// Issue #24: auto-apply planner-suggested labels before announcing plan_ready
	// so the modal opens with the chips already in their final state.
	if ws.SkillProfile.AutoApplyLabelsEnabled() && len(plan.SuggestedLabels) > 0 {
		a.autoApplyPlanLabels(ws, sess.IssueNumber, plan)
	}
	a.bus.Publish(eventbus.EvtPlanReady, map[string]any{
		"workspace_id": sess.WorkspaceID,
		"issue_number": sess.IssueNumber,
		"revision":     plan.Revision,
	})
	// Move the card to the PLAN column if it isn't there already.
	_ = a.store.MoveIssueColumn(sess.WorkspaceID, sess.IssueNumber, types.ColPlan)
	if a.notificationsSuppressed() {
		return
	}
	a.emitToast("info", toastWorkspaceName(a.wsReg, sess.WorkspaceID),
		fmt.Sprintf("#%d plan ready (rev %d, %d questions)", sess.IssueNumber, plan.Revision, len(plan.Questions)),
		map[string]any{
			"workspace_id": sess.WorkspaceID,
			"issue_number": sess.IssueNumber,
			"action":       "open_plan",
		})
}

// pullNumberRegexp extracts the PR number from a github.com pull URL.
// Compiled once at package init.
var pullNumberRegexp = regexp.MustCompile(`/pull/(\d+)`)

func pullNumberFromURL(url string) (int, bool) {
	m := pullNumberRegexp.FindStringSubmatch(url)
	if len(m) < 2 {
		return 0, false
	}
	n, err := strconv.Atoi(m[1])
	if err != nil {
		return 0, false
	}
	return n, true
}

// handlePROpened persists the PR number + URL on the issue row, moves the
// card to REVIEW, and fires an OS notification when a worker emits the
// "PR_OPENED: <url>" sentinel.
func (a *App) handlePROpened(sess types.Session, prURL string) {
	if a.wsReg == nil || a.store == nil {
		return
	}
	prURL = strings.TrimSpace(prURL)
	if prURL == "" {
		log.Printf("PR_OPENED: empty URL on session %s, skipping", sess.ID)
		a.markSessionBlocked(sess, "PR_OPENED sentinel had empty URL")
		return
	}
	n, ok := pullNumberFromURL(prURL)
	if !ok {
		log.Printf("PR_OPENED: could not parse PR number from %q (session %s), skipping", prURL, sess.ID)
		a.markSessionBlocked(sess, fmt.Sprintf("PR_OPENED URL %q had no /pull/<n> segment", prURL))
		return
	}
	if _, ok := a.wsReg.Get(sess.WorkspaceID); !ok {
		log.Printf("PR_OPENED: unknown workspace %q (session %s), skipping", sess.WorkspaceID, sess.ID)
		return
	}
	if err := a.store.MarkPROpened(sess.WorkspaceID, sess.IssueNumber, n, prURL); err != nil {
		log.Printf("PR_OPENED: MarkPROpened failed for #%d: %v", sess.IssueNumber, err)
		a.markSessionBlocked(sess, fmt.Sprintf("PR_OPENED store update failed: %v", err))
		return
	}
	a.bus.Publish(eventbus.EvtPROpened, map[string]any{
		"workspace_id": sess.WorkspaceID,
		"issue_number": sess.IssueNumber,
		"pr_number":    n,
		"pr_url":       prURL,
	})
	if !a.notificationsSuppressed() {
		a.emitToast("success", toastWorkspaceName(a.wsReg, sess.WorkspaceID),
			fmt.Sprintf("#%d opened PR #%d", sess.IssueNumber, n),
			map[string]any{
				"workspace_id": sess.WorkspaceID,
				"issue_number": sess.IssueNumber,
				"pr_url":       prURL,
				"action":       "open_pr",
			})
	}
	// Auto-kill the execute worker now that the PR is open (#118). The
	// worktree is preserved (session lands in Completed) so Continue Work can
	// resume. Emit a toast only when the kill succeeded so we surface real impact.
	killID, killStart, costCents, killed := a.mgr.KillAfterPROpened(sess.WorkspaceID, sess.IssueNumber)
	if killed {
		dur := time.Since(killStart).Round(time.Second)
		mins := int(dur.Minutes())
		secs := int(dur.Seconds()) % 60
		var durStr string
		if mins > 0 {
			durStr = fmt.Sprintf("%dm %ds", mins, secs)
		} else {
			durStr = fmt.Sprintf("%ds", secs)
		}
		log.Printf("auto-cancel: PR #%d opened; killing execute session %s for issue #%d (was running %s)",
			n, killID, sess.IssueNumber, durStr)
		if !a.notificationsSuppressed() {
			a.emitToast("info", toastWorkspaceName(a.wsReg, sess.WorkspaceID),
				fmt.Sprintf("#%d: PR opened — stopping execute worker (was running %s, ~$%.2f)",
					sess.IssueNumber, durStr, costCents/100),
				map[string]any{
					"workspace_id": sess.WorkspaceID,
					"issue_number": sess.IssueNumber,
				})
		}
	}
}

// handleNeedsPR is called when an execute worker emits the NEEDS_PR: sentinel
// (#157). Persists NeedsPRInfo on the issue, moves the card to REVIEW, and
// emits a toast so the user knows to push manually.
func (a *App) handleNeedsPR(sess types.Session, branch, worktreeDir, reason string) {
	if a.store == nil {
		return
	}
	// Derive kind from reason text: prefer "commit_signing" when the reason
	// mentions a signing/gpg failure; fall through to "push" for everything else.
	kind := "push"
	for _, sig := range []string{"gpg", "signing", "sign", "secret key", "no secret key"} {
		if strings.Contains(strings.ToLower(reason), sig) {
			kind = "commit_signing"
			break
		}
	}
	// Read the Tier-3 commit message file written by the worker (issue #175).
	// The worker writes it to <worktreeDir>/.prismconductor/commit-msg/<issueNum>.txt.
	commitMsgFile := filepath.Join(worktreeDir, ".prismconductor", "commit-msg",
		fmt.Sprintf("%d.txt", sess.IssueNumber))
	var commitMsg string
	if raw, err := os.ReadFile(commitMsgFile); err == nil {
		if len(raw) > 500 {
			commitMsg = string(raw[:500])
		} else {
			commitMsg = string(raw)
		}
	} else {
		commitMsgFile = "" // file absent — clear path so UI knows it's missing
	}
	info := types.NeedsPRInfo{
		Branch:        branch,
		WorktreeDir:   worktreeDir,
		Reason:        reason,
		Kind:          kind,
		CommitMsgFile: commitMsgFile,
		CommitMsg:     commitMsg,
	}
	if err := a.store.MarkNeedsPR(sess.WorkspaceID, sess.IssueNumber, info); err != nil {
		log.Printf("NEEDS_PR: MarkNeedsPR failed for #%d: %v", sess.IssueNumber, err)
		return
	}
	a.bus.Publish(eventbus.EvtNeedsPR, eventbus.NeedsPREvent{
		WorkspaceID: sess.WorkspaceID,
		IssueNumber: sess.IssueNumber,
		Branch:      branch,
		WorktreeDir: worktreeDir,
		Reason:      reason,
		Kind:        kind,
	})
	if a.notificationsSuppressed() {
		return
	}
	a.emitToast("warning", toastWorkspaceName(a.wsReg, sess.WorkspaceID),
		fmt.Sprintf("#%d needs manual push — commits ready in worktree", sess.IssueNumber),
		map[string]any{
			"workspace_id": sess.WorkspaceID,
			"issue_number": sess.IssueNumber,
			"action":       "focus_card",
		})
}

// PrepareManualPushCommand returns the exact shell command the user should paste
// to complete a NEEDS_PR card's commit+push from the preserved worktree (#175).
func (a *App) PrepareManualPushCommand(workspaceID string, issueNumber int) (string, error) {
	if a.store == nil {
		return "", fmt.Errorf("store unavailable")
	}
	issue, err := a.store.LoadIssue(workspaceID, issueNumber)
	if err != nil {
		return "", fmt.Errorf("issue not found: %w", err)
	}
	if issue.NeedsPRInfo == nil {
		return "", fmt.Errorf("issue #%d has no NEEDS_PR info", issueNumber)
	}
	return handlers.BuildManualPushCommand(issue.NeedsPRInfo), nil
}

// AttachManualPR allows the user to paste a PR URL for a NEEDS_PR card,
// immediately attaching it without waiting for the next poll cycle (#157).
// The PR is validated against the GitHub API before persisting.
func (a *App) AttachManualPR(workspaceID string, issueNumber int, prURL string) error {
	if a.store == nil || a.wsReg == nil {
		return fmt.Errorf("store unavailable")
	}
	prURL = strings.TrimSpace(prURL)
	n, ok := pullNumberFromURL(prURL)
	if !ok {
		return fmt.Errorf("could not parse PR number from %q", prURL)
	}
	ws, ok := a.wsReg.Get(workspaceID)
	if !ok {
		return fmt.Errorf("unknown workspace %q", workspaceID)
	}
	// Validate the PR exists on GitHub before persisting.
	if a.gh != nil {
		if _, err := a.gh.FetchPRState(a.ctx, ws, n); err != nil {
			return fmt.Errorf("PR not found on GitHub: %w", err)
		}
	}
	if err := a.store.MarkPROpened(workspaceID, issueNumber, n, prURL); err != nil {
		return fmt.Errorf("attach PR: %w", err)
	}
	a.bus.Publish(eventbus.EvtPROpened, map[string]any{
		"workspace_id": workspaceID,
		"issue_number": issueNumber,
		"pr_number":    n,
		"pr_url":       prURL,
	})
	return nil
}

// notifyOnPRStateChange fires an in-app toast when the poller publishes
// a pr_merged or pr_closed_unmerged event (#33). The poller can't emit
// toasts directly without an awkward layering import; the bus.Subscribe
// callback is the cleanest seam. Honors the same mute/quiet-hours gate as
// session-state toasts.
func (a *App) notifyOnPRStateChange(e eventbus.Event) {
	if e.Type != eventbus.EvtPRMerged && e.Type != eventbus.EvtPRClosedUnmerged {
		return
	}
	if a.notificationsSuppressed() {
		return
	}
	payload, _ := e.Payload.(map[string]any)
	if payload == nil {
		return
	}
	wsID, _ := payload["workspace_id"].(string)
	issNumF, _ := payload["issue_number"].(float64)
	issNum := int(issNumF)
	if v, ok := payload["issue_number"].(int); ok {
		issNum = v
	}
	prURL, _ := payload["pr_url"].(string)
	title := toastWorkspaceName(a.wsReg, wsID)
	var (
		body   string
		level  string
		action string
	)
	switch e.Type {
	case eventbus.EvtPRMerged:
		body = fmt.Sprintf("#%d merged → DONE", issNum)
		level = "success"
		action = "open_pr"
	case eventbus.EvtPRClosedUnmerged:
		body = fmt.Sprintf("#%d PR closed without merge", issNum)
		level = "error"
		action = "focus_card"
	}
	toastPayload := map[string]any{
		"workspace_id": wsID,
		"issue_number": issNum,
		"action":       action,
	}
	if prURL != "" {
		toastPayload["pr_url"] = prURL
	}
	a.emitToast(level, title, body, toastPayload)

	// Auto-kill any running execute session that opened this PR (#113).
	if a.mgr != nil {
		if sessID, startedAt, ok := a.mgr.FindActiveExecuteSession(wsID, issNum); ok {
			elapsed := time.Since(startedAt).Round(time.Second)
			var autoBody string
			switch e.Type {
			case eventbus.EvtPRMerged:
				autoBody = fmt.Sprintf("#%d: PR merged — stopping execute worker (was running %s)", issNum, elapsed)
			case eventbus.EvtPRClosedUnmerged:
				autoBody = fmt.Sprintf("#%d: PR closed — stopping execute worker (was running %s)", issNum, elapsed)
			}
			log.Printf("auto-cancel: PR event %s; killing execute session %s for issue #%d (ran %s)", e.Type, sessID, issNum, elapsed)
			_ = a.mgr.Kill(sessID)
			a.emitToast("info", title, autoBody, toastPayload)
		}
	}
}

// GetIssueView returns the canonical IssueView for a single issue (#98).
func (a *App) GetIssueView(workspaceID string, issueNumber int) (issueview.IssueView, error) {
	if a.assembler == nil {
		return issueview.IssueView{}, fmt.Errorf("assembler unavailable")
	}
	return a.assembler.Assemble(workspaceID, issueNumber)
}

// ListIssueViews returns canonical IssueViews for every non-archived issue
// in the workspace (#98). Used by the frontend on workspace load to populate
// the IssueView store before incremental bus.issue_view_updated events arrive.
func (a *App) ListIssueViews(workspaceID string) ([]issueview.IssueView, error) {
	if a.assembler == nil {
		return nil, fmt.Errorf("assembler unavailable")
	}
	return a.assembler.ListForWorkspace(workspaceID)
}

// LatestPlan returns the highest-revision plan for an issue.
func (a *App) LatestPlan(workspaceID string, issueNumber int) (*types.Plan, error) {
	if a.store == nil {
		return nil, fmt.Errorf("store unavailable")
	}
	return a.store.LatestPlan(workspaceID, issueNumber)
}

// ListPlans returns every revision for an issue, oldest first.
func (a *App) ListPlans(workspaceID string, issueNumber int) ([]types.Plan, error) {
	if a.store == nil {
		return nil, fmt.Errorf("store unavailable")
	}
	return a.store.ListPlans(workspaceID, issueNumber)
}

// ListPendingPlans returns every issue whose latest plan revision has not
// been approved yet. Frontend calls this on mount to rehydrate the
// "plan ready (rev N)" glow on cards that had a plan ready when the user
// last quit the app.
func (a *App) ListPendingPlans() ([]store.PendingPlan, error) {
	if a.store == nil {
		return nil, fmt.Errorf("store unavailable")
	}
	return a.store.ListPendingPlans()
}

// midRunQuestionContext mirrors the sidecar the conductor-question skill writes
// alongside `<id>.json`. The plan_path is repo-relative; the resume callback
// uses revision to look up the plan in the store.
type midRunQuestionContext struct {
	IssueNumber int    `json:"issue_number"`
	WorkspaceID string `json:"workspace_id"`
	Revision    int    `json:"revision"`
	Branch      string `json:"branch"`
	PlanPath    string `json:"plan_path"`
	Scratch     string `json:"scratch"`
}

// handleMidRunAnswerArrived is the watcher callback (#17). Loads the plan +
// issue, spawns a resume execute worker on the same branch, and clears the
// pending_question_id on the OLD session row so subsequent ticks don't re-fire
// on the stale match.
func (a *App) handleMidRunAnswerArrived(ps store.PausedSession) {
	if a.wsReg == nil || a.store == nil || a.mgr == nil {
		return
	}
	ws, ok := a.wsReg.Get(ps.WorkspaceID)
	if !ok {
		log.Printf("answer arrived for unknown workspace %q (session %s)", ps.WorkspaceID, ps.SessionID)
		return
	}
	ctxPath := filepath.Join(ws.RepoPath, ".prismconductor", "questions", ps.QuestionID+".context.json")
	raw, err := os.ReadFile(ctxPath)
	if err != nil {
		log.Printf("answer arrived but context sidecar unreadable at %s: %v", ctxPath, err)
		return
	}
	var ctx midRunQuestionContext
	if err := json.Unmarshal(raw, &ctx); err != nil {
		log.Printf("invalid context sidecar at %s: %v", ctxPath, err)
		return
	}
	plan, err := a.store.GetPlan(ws.ID, ps.IssueNumber, ctx.Revision)
	if err != nil {
		log.Printf("answer resume: load plan rev %d for #%d: %v", ctx.Revision, ps.IssueNumber, err)
		return
	}
	issue, err := a.store.LoadIssue(ws.ID, ps.IssueNumber)
	if err != nil {
		log.Printf("answer resume: load issue #%d (%v); falling back to stub", ps.IssueNumber, err)
		issue = types.Issue{Number: ps.IssueNumber, WorkspaceID: ws.ID}
	}
	// Clear the pending_question_id on the OLD session BEFORE spawning so the
	// watcher's next tick doesn't see this paused row again. The new session
	// is the source of truth from here on.
	if err := a.store.UpdateSessionPendingQuestion(ps.SessionID, ""); err != nil {
		log.Printf("answer resume: clear pending_question_id on %s: %v", ps.SessionID, err)
	}
	// Mark the OLD session terminal so the card no longer renders the orange
	// "paused_for_question" state. Without this, both the old (paused) and
	// new (running) sessions exist for the same issue and the frontend's
	// pausedSession selector keeps the card stuck in the awaiting-answer
	// look even though work has resumed. The old session's row is sealed —
	// `completed` is the natural terminal state since the question round-
	// trip succeeded and the new session inherits the work.
	if err := a.store.UpdateSessionState(ps.SessionID, types.StateCompleted); err != nil {
		log.Printf("answer resume: seal old session %s: %v", ps.SessionID, err)
	} else {
		// Push a synthesized session.state event so the frontend's session
		// store refreshes the OLD row's state from `paused_for_question` to
		// `completed`. Without this, the Card's pausedSession selector keeps
		// the orange overlay until the next Wails reload.
		wruntime.EventsEmit(a.ctx, "session.state", types.Session{
			ID:          ps.SessionID,
			WorkspaceID: ps.WorkspaceID,
			IssueNumber: ps.IssueNumber,
			Mode:        types.ModeExecute,
			State:       types.StateCompleted,
		})
	}
	pool, ok := a.acquireWorkPool(ws)
	if !ok {
		log.Printf("answer resume: no work pool available for #%d", ps.IssueNumber)
		return
	}
	if _, err := a.mgr.SpawnExecuteResume(ws, issue, plan, pool, ps.QuestionID); err != nil {
		a.poolReg.ReleaseByPool(pool.ID)
		log.Printf("answer resume: SpawnExecuteResume #%d: %v", ps.IssueNumber, err)
		return
	}
	log.Printf("answer resume: spawned execute worker for #%d on branch %s (qid=%s)",
		ps.IssueNumber, ctx.Branch, ps.QuestionID)
}

// handleOrphanQuestion is the AnswerWatcher callback for orphaned paused
// sessions (#153). Auto-recover fires only when the workspace setting
// `auto_recover_orphan_questions:<wsID>` is not explicitly set to "false".
func (a *App) handleOrphanQuestion(ps store.PausedSession) {
	if a.store == nil {
		return
	}
	v, _ := a.store.GetSetting("auto_recover_orphan_questions:" + ps.WorkspaceID)
	if v == "false" {
		return
	}
	sess, err := a.store.TerminateOrphanPausedSession(ps.WorkspaceID, ps.IssueNumber,
		"question file gone — auto-recovered after 60s")
	if err != nil {
		log.Printf("auto-recover orphan #%d: %v", ps.IssueNumber, err)
		return
	}
	if sess == nil {
		return
	}
	if sess.PoolID != "" {
		a.bus.Publish(eventbus.EvtWorkerSlotFreed, eventbus.WorkerSlotFreed{
			SessionID: sess.ID,
			PoolID:    sess.PoolID,
		})
	}
	if a.ctx != nil {
		wruntime.EventsEmit(a.ctx, "session.state", *sess)
	}
	if a.bus != nil {
		a.bus.Publish(eventbus.EvtSessionStateChanged, eventbus.SessionStateChanged{
			WorkspaceID: sess.WorkspaceID,
			IssueNumber: sess.IssueNumber,
			SessionID:   sess.ID,
		})
	}
	log.Printf("auto-recovered orphan paused session %s for #%d", sess.ID[:8], ps.IssueNumber)
}

// RecoverOrphanQuestion manually marks an orphaned paused_for_question session
// as failed and releases its worker slot (#153). Called from the card's Recover
// button when OrphanQuestion is set in the IssueView.
func (a *App) RecoverOrphanQuestion(workspaceID string, issueNumber int) error {
	if a.store == nil {
		return fmt.Errorf("store unavailable")
	}
	if a.wsReg != nil {
		if _, ok := a.wsReg.Get(workspaceID); !ok {
			return fmt.Errorf("unknown workspace %q", workspaceID)
		}
	}
	sess, err := a.store.TerminateOrphanPausedSession(workspaceID, issueNumber,
		"question file missing — recovered manually")
	if err != nil {
		return err
	}
	if sess == nil {
		// No orphan found — force a reassemble so the frontend's view converges.
		if a.assembler != nil {
			a.assembler.Reassemble(workspaceID, issueNumber)
		}
		return nil
	}
	if sess.PoolID != "" {
		a.bus.Publish(eventbus.EvtWorkerSlotFreed, eventbus.WorkerSlotFreed{
			SessionID: sess.ID,
			PoolID:    sess.PoolID,
		})
	}
	if a.ctx != nil {
		wruntime.EventsEmit(a.ctx, "session.state", *sess)
	}
	if a.bus != nil {
		a.bus.Publish(eventbus.EvtSessionStateChanged, eventbus.SessionStateChanged{
			WorkspaceID: sess.WorkspaceID,
			IssueNumber: sess.IssueNumber,
			SessionID:   sess.ID,
		})
	}
	log.Printf("RecoverOrphanQuestion: recovered session %s for %s#%d", sess.ID[:8], workspaceID, issueNumber)
	return nil
}

// ReplanForce cancels any non-terminal plan-mode sessions for the given issue
// (running, waiting_for_input, paused_for_question) then spawns a fresh plan
// worker (#153, Q1=A: only plan-mode sessions; execute sessions are left alone).
func (a *App) ReplanForce(workspaceID string, number int) error {
	if a.wsReg == nil || a.mgr == nil {
		return fmt.Errorf("registry/manager unavailable")
	}
	ws, ok := a.wsReg.Get(workspaceID)
	if !ok {
		return fmt.Errorf("unknown workspace %q", workspaceID)
	}
	// Cancel live plan sessions (running / waiting_for_input / blocked).
	for _, s := range a.mgr.Snapshot() {
		if s.WorkspaceID != workspaceID || s.IssueNumber != number || s.Mode != types.ModePlan {
			continue
		}
		switch s.State {
		case types.StateRunning, types.StateWaitingForInput, types.StateBlocked:
			if err := a.mgr.KillGraceful(s.ID); err != nil {
				log.Printf("ReplanForce: kill session %s: %v", s.ID[:8], err)
			}
		}
	}
	// Terminate any paused_for_question plan session.
	if a.store != nil {
		if _, err := a.store.TerminateOrphanPausedSession(workspaceID, number, "force-replan by user"); err != nil {
			log.Printf("ReplanForce: terminate paused session for #%d: %v", number, err)
		}
	}
	log.Printf("ReplanForce: spawning plan worker for %s#%d", workspaceID, number)
	pool, ok := a.acquirePlanPool(ws)
	if !ok {
		return fmt.Errorf("no plan pool available")
	}
	sess, err := a.mgr.SpawnPlan(ws, types.Issue{Number: number, WorkspaceID: workspaceID}, pool)
	if err != nil {
		a.poolReg.ReleaseByPool(pool.ID)
		log.Printf("ReplanForce #%d FAILED: %v", number, err)
		return err
	}
	log.Printf("ReplanForce: spawn ok for #%d, session=%s pid=%d pool=%s", number, sess.ID[:8], sess.PID, pool.ID)
	return nil
}

// SubmitMidRunAnswer writes the user's answer for a mid-run question (#17).
// One question per call; the file lives at
// `<RepoPath>/.prismconductor/answers/<question_id>.json` (NOT keyed by issue
// or revision — mid-run questions are indexed by their own UUID). The
// AnswerWatcher trips the resume on its next tick.
func (a *App) SubmitMidRunAnswer(workspaceID string, issueNumber int, answer types.MidRunAnswer) error {
	if a.wsReg == nil {
		return fmt.Errorf("workspace registry unavailable")
	}
	ws, ok := a.wsReg.Get(workspaceID)
	if !ok {
		return fmt.Errorf("unknown workspace %q", workspaceID)
	}
	if answer.QuestionID == "" {
		return fmt.Errorf("question_id required")
	}
	dir := filepath.Join(ws.RepoPath, ".prismconductor", "answers")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	b, err := json.MarshalIndent(answer, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, answer.QuestionID+".json"), b, 0o644)
}

// GetMidRunQuestion reads the on-disk question definition (#17) so the
// frontend modal can render without direct fs access. The question lives at
// `<RepoPath>/.prismconductor/questions/<question_id>.json`.
func (a *App) GetMidRunQuestion(workspaceID string, issueNumber int, questionID string) (types.Question, error) {
	if a.wsReg == nil {
		return types.Question{}, fmt.Errorf("workspace registry unavailable")
	}
	ws, ok := a.wsReg.Get(workspaceID)
	if !ok {
		return types.Question{}, fmt.Errorf("unknown workspace %q", workspaceID)
	}
	if questionID == "" {
		return types.Question{}, fmt.Errorf("question_id required")
	}
	path := filepath.Join(ws.RepoPath, ".prismconductor", "questions", questionID+".json")
	raw, err := os.ReadFile(path)
	if err != nil {
		return types.Question{}, err
	}
	var q types.Question
	if err := json.Unmarshal(raw, &q); err != nil {
		return types.Question{}, fmt.Errorf("invalid question JSON at %s: %w", path, err)
	}
	return q, nil
}

// AnswerSubmission is the frontend's payload for the answers form.
type AnswerSubmission struct {
	WorkspaceID string              `json:"workspace_id"`
	IssueNumber int                 `json:"issue_number"`
	Revision    int                 `json:"revision"`
	Answers     map[string]string   `json:"answers"`
	Multi       map[string][]string `json:"multi"`
}

// WriteAnswersOnly persists the answers file without spawning a re-plan.
// Frontend calls this when the user clicks "Approve & execute" with answers
// filled in: the execute skill picks the answers up at the same revision,
// and we save the cost of another paid planner round-trip on a model that
// likely produces the same questions on rev2/rev3 anyway (issue #67's
// follow-up, observed in the wild on gpt-5-mini via OpenRouter).
func (a *App) WriteAnswersOnly(sub AnswerSubmission) error {
	if a.wsReg == nil || a.store == nil {
		return fmt.Errorf("registry/store unavailable")
	}
	ws, ok := a.wsReg.Get(sub.WorkspaceID)
	if !ok {
		return fmt.Errorf("unknown workspace %q", sub.WorkspaceID)
	}
	if err := planio.WriteAnswers(ws.RepoPath, sub.IssueNumber, sub.Revision, planio.AnswerSet{
		Answers: sub.Answers,
		Multi:   sub.Multi,
	}); err != nil {
		return err
	}
	return nil
}

// SubmitAnswers writes the answers file and re-spawns plan mode for the next
// revision. Frontend calls this after the user fills the QuestionForm.
func (a *App) SubmitAnswers(sub AnswerSubmission) error {
	if a.wsReg == nil || a.store == nil {
		return fmt.Errorf("registry/store unavailable")
	}
	ws, ok := a.wsReg.Get(sub.WorkspaceID)
	if !ok {
		return fmt.Errorf("unknown workspace %q", sub.WorkspaceID)
	}
	if err := planio.WriteAnswers(ws.RepoPath, sub.IssueNumber, sub.Revision, planio.AnswerSet{
		Answers: sub.Answers,
		Multi:   sub.Multi,
	}); err != nil {
		return err
	}
	// Re-spawn plan worker; the bundled skill checks for an answers file at the
	// matching revision and emits rev<N+1>.
	pool, ok := a.acquirePlanPool(ws)
	if !ok {
		return fmt.Errorf("no plan pool available")
	}
	if _, err := a.mgr.SpawnPlan(ws, types.Issue{Number: sub.IssueNumber, WorkspaceID: sub.WorkspaceID}, pool); err != nil {
		a.poolReg.ReleaseByPool(pool.ID)
		return err
	}
	a.bus.Publish(eventbus.EvtPlanRevised, map[string]any{
		"workspace_id": sub.WorkspaceID,
		"issue_number": sub.IssueNumber,
		"from_revision": sub.Revision,
	})
	return nil
}

// ApprovePlan stamps approved_at on the given revision, moves the card to
// IN_PROGRESS, and spawns the execute-mode worker.
func (a *App) ApprovePlan(workspaceID string, issueNumber, revision int) error {
	if a.wsReg == nil || a.store == nil {
		return fmt.Errorf("registry/store unavailable")
	}
	ws, ok := a.wsReg.Get(workspaceID)
	if !ok {
		return fmt.Errorf("unknown workspace %q", workspaceID)
	}
	plan, err := a.store.GetPlan(workspaceID, issueNumber, revision)
	if err != nil {
		return err
	}
	now := time.Now()
	plan.ApprovedAt = &now
	plan.ReadyToExecute = true
	if err := a.store.SavePlan(plan); err != nil {
		return err
	}
	if err := a.store.MoveIssueColumn(workspaceID, issueNumber, types.ColInProgress); err != nil {
		return err
	}
	// Issue #22: SpawnExecute derives the branch slug from Issue.Title, so we
	// load the full row instead of passing a stub. Fall back to the stub if
	// the issue isn't in the store (e.g., conductor-only test rows that
	// somehow lost their record), so an approved plan still spawns.
	issue, err := a.store.LoadIssue(workspaceID, issueNumber)
	if err != nil {
		log.Printf("ApprovePlan: load issue #%d failed (%v); spawning with empty title", issueNumber, err)
		issue = types.Issue{Number: issueNumber, WorkspaceID: workspaceID}
	}
	pool, ok := a.acquireWorkPool(ws)
	if !ok {
		// No work slot available — persist intent and show waiting decoration.
		// The card stays in IN_PROGRESS; drain fires it when a slot frees.
		_ = a.store.EnqueuePendingPool(workspaceID, issueNumber, types.RoleWork, "approve_execute")
		_ = a.store.SetIssueWaitingForPool(workspaceID, issueNumber, true)
		a.bus.Publish(eventbus.EvtPendingPoolEnqueued, eventbus.PendingPoolChange{
			WorkspaceID: workspaceID,
			IssueNumber: issueNumber,
			Role:        string(types.RoleWork),
		})
		return nil
	}
	if _, err := a.mgr.SpawnExecute(ws, issue, plan, pool); err != nil {
		a.handleSpawnExecuteFailure(workspaceID, issueNumber, pool, err)
		return err
	}
	// Issue #101 Q3: pre-flight estimate toast so the user sees expected spend
	// before the worker actually incurs it.
	est := a.EstimateSpawnCost(workspaceID, issueNumber, pool.ID)
	if est.Tokens > 0 {
		var costStr string
		if est.CostCents > 0 {
			costStr = fmt.Sprintf(", est. $%.2f", est.CostCents/100)
		}
		a.emitToast("info", toastWorkspaceName(a.wsReg, workspaceID),
			fmt.Sprintf("#%d execute spawned on %s (~%s tok%s)",
				issueNumber, pool.Model,
				formatTokenCount(est.Tokens), costStr),
			map[string]any{"workspace_id": workspaceID, "issue_number": issueNumber, "action": "focus_card"})
	}
	a.bus.Publish(eventbus.EvtPlanApproved, map[string]any{
		"workspace_id": workspaceID,
		"issue_number": issueNumber,
		"revision":     revision,
	})
	return nil
}

// handleSpawnExecuteFailure undoes the IN_PROGRESS column move, sets a
// failure_reason on the issue so the card shows a retry affordance, releases
// the pool slot, and emits a top-level toast. Shared by ApprovePlan and
// RetryExecuteForApprovedPlan.
func (a *App) handleSpawnExecuteFailure(workspaceID string, issueNumber int, pool types.Pool, spawnErr error) {
	a.poolReg.ReleaseByPool(pool.ID)
	if rerr := a.store.MoveIssueColumn(workspaceID, issueNumber, types.ColPlan); rerr != nil {
		log.Printf("handleSpawnExecuteFailure: rollback column for #%d: %v", issueNumber, rerr)
	}
	if rerr := a.store.SetIssueFailureReason(workspaceID, issueNumber,
		fmt.Sprintf("execute spawn failed: %v", spawnErr)); rerr != nil {
		log.Printf("handleSpawnExecuteFailure: set failure_reason for #%d: %v", issueNumber, rerr)
	}
	a.emitToast("error", toastWorkspaceName(a.wsReg, workspaceID),
		fmt.Sprintf("#%d execute could not start — %v", issueNumber, spawnErr),
		map[string]any{"workspace_id": workspaceID, "issue_number": issueNumber, "action": "focus_card"})
}

// RetryExecuteForApprovedPlan re-attempts SpawnExecute against the existing
// approved plan for an issue that was rolled back to PLAN after a prior spawn
// failure. Clears failure_reason on success.
func (a *App) RetryExecuteForApprovedPlan(workspaceID string, issueNumber int) error {
	if a.wsReg == nil || a.store == nil {
		return fmt.Errorf("registry/store unavailable")
	}
	ws, ok := a.wsReg.Get(workspaceID)
	if !ok {
		return fmt.Errorf("unknown workspace %q", workspaceID)
	}
	plan, err := a.store.LatestPlan(workspaceID, issueNumber)
	if err != nil {
		return err
	}
	if plan == nil || plan.ApprovedAt == nil {
		return fmt.Errorf("no approved plan found for issue #%d", issueNumber)
	}
	issue, err := a.store.LoadIssue(workspaceID, issueNumber)
	if err != nil {
		log.Printf("RetryExecuteForApprovedPlan: load issue #%d failed (%v); spawning with empty title", issueNumber, err)
		issue = types.Issue{Number: issueNumber, WorkspaceID: workspaceID}
	}
	pool, ok := a.acquireWorkPool(ws)
	if !ok {
		_ = a.store.EnqueuePendingPool(workspaceID, issueNumber, types.RoleWork, "retry_execute")
		_ = a.store.SetIssueWaitingForPool(workspaceID, issueNumber, true)
		a.bus.Publish(eventbus.EvtPendingPoolEnqueued, eventbus.PendingPoolChange{
			WorkspaceID: workspaceID,
			IssueNumber: issueNumber,
			Role:        string(types.RoleWork),
		})
		return nil
	}
	if err := a.store.MoveIssueColumn(workspaceID, issueNumber, types.ColInProgress); err != nil {
		a.poolReg.ReleaseByPool(pool.ID)
		return err
	}
	if _, err := a.mgr.SpawnExecute(ws, issue, *plan, pool); err != nil {
		a.handleSpawnExecuteFailure(workspaceID, issueNumber, pool, err)
		return err
	}
	_ = a.store.SetIssueFailureReason(workspaceID, issueNumber, "")
	est := a.EstimateSpawnCost(workspaceID, issueNumber, pool.ID)
	if est.Tokens > 0 {
		var costStr string
		if est.CostCents > 0 {
			costStr = fmt.Sprintf(", est. $%.2f", est.CostCents/100)
		}
		a.emitToast("info", toastWorkspaceName(a.wsReg, workspaceID),
			fmt.Sprintf("#%d execute spawned on %s (~%s tok%s)",
				issueNumber, pool.Model,
				formatTokenCount(est.Tokens), costStr),
			map[string]any{"workspace_id": workspaceID, "issue_number": issueNumber, "action": "focus_card"})
	}
	a.bus.Publish(eventbus.EvtPlanApproved, map[string]any{
		"workspace_id": workspaceID,
		"issue_number": issueNumber,
		"revision":     plan.Revision,
	})
	return nil
}

// formatTokenCount formats a token count as a human-readable string.
func formatTokenCount(n int64) string {
	if n >= 1_000_000 {
		return fmt.Sprintf("%.1fM", float64(n)/1_000_000)
	}
	if n >= 1_000 {
		return fmt.Sprintf("%.1fK", float64(n)/1_000)
	}
	return fmt.Sprintf("%d", n)
}

// ContinueWork spawns an execute worker to continue existing PR work after
// review feedback or test failures. The note describes what to fix; it is
// written to .prismconductor/notes/<issueNumber>.txt and mirrored into the
// feature-branch worktree for the conductor-continue skill to read.
func (a *App) ContinueWork(workspaceID string, issueNumber int, note string) error {
	if a.wsReg == nil || a.store == nil || a.mgr == nil {
		return fmt.Errorf("registry/store/manager unavailable")
	}
	if strings.TrimSpace(note) == "" {
		return fmt.Errorf("note is required")
	}
	ws, ok := a.wsReg.Get(workspaceID)
	if !ok {
		return fmt.Errorf("unknown workspace %q", workspaceID)
	}
	issue, err := a.store.LoadIssue(workspaceID, issueNumber)
	if err != nil {
		return fmt.Errorf("load issue: %w", err)
	}
	if issue.Column != types.ColReview {
		return fmt.Errorf("issue #%d is not in REVIEW column (got %q)", issueNumber, issue.Column)
	}
	if a.hasActiveExecuteSession(workspaceID, issueNumber) {
		return fmt.Errorf("issue #%d already has an active execute session", issueNumber)
	}
	plan, err := a.store.LatestPlan(workspaceID, issueNumber)
	if err != nil {
		return fmt.Errorf("load plan: %w", err)
	}
	if plan == nil {
		return fmt.Errorf("no plan found for issue #%d", issueNumber)
	}
	notesDir := filepath.Join(ws.RepoPath, ".prismconductor", "notes")
	if err := os.MkdirAll(notesDir, 0o755); err != nil {
		return fmt.Errorf("create notes dir: %w", err)
	}
	notePath := filepath.Join(notesDir, fmt.Sprintf("%d.txt", issueNumber))
	if err := os.WriteFile(notePath, []byte(note), 0o644); err != nil {
		return fmt.Errorf("write note: %w", err)
	}
	if err := a.store.MoveIssueColumn(workspaceID, issueNumber, types.ColInProgress); err != nil {
		return err
	}
	pool, ok := a.acquireWorkPool(ws)
	if !ok {
		_ = a.store.EnqueuePendingPool(workspaceID, issueNumber, types.RoleWork, "continue_execute")
		_ = a.store.SetIssueWaitingForPool(workspaceID, issueNumber, true)
		a.bus.Publish(eventbus.EvtPendingPoolEnqueued, eventbus.PendingPoolChange{
			WorkspaceID: workspaceID,
			IssueNumber: issueNumber,
			Role:        string(types.RoleWork),
		})
		return nil
	}
	if _, err := a.mgr.SpawnExecuteContinue(ws, issue, *plan, pool); err != nil {
		a.poolReg.ReleaseByPool(pool.ID)
		_ = a.store.MoveIssueColumn(workspaceID, issueNumber, types.ColReview)
		return err
	}
	a.bus.Publish(eventbus.EvtPlanApproved, map[string]any{
		"workspace_id": workspaceID,
		"issue_number": issueNumber,
		"revision":     plan.Revision,
	})
	return nil
}

// SelfHeal spawns a Continue-Work session pre-populated with CI failure context
// from the most recent EvtPRChecksFailed event. Returns an error if the attempt
// cap is reached or no check-failure state is recorded (#116).
func (a *App) SelfHeal(workspaceID string, issueNumber int) error {
	if a.assembler == nil {
		return fmt.Errorf("assembler unavailable")
	}
	view, err := a.assembler.Assemble(workspaceID, issueNumber)
	if err != nil {
		return fmt.Errorf("assemble issue view: %w", err)
	}
	tfi := view.TestsFailingInfo
	if tfi == nil {
		return fmt.Errorf("no CI failure recorded for issue #%d", issueNumber)
	}
	if tfi.MaxAttemptsReached {
		return fmt.Errorf("max self-heal attempts (%d) reached for issue #%d — review manually", tfi.AttemptCap, issueNumber)
	}
	// Build the CI failure note for the continue worker.
	note := a.buildSelfHealNote(tfi)
	if err := a.ContinueWork(workspaceID, issueNumber, note); err != nil {
		return err
	}
	// Increment attempt counter and update the assembler.
	ws, _ := a.wsReg.Get(workspaceID)
	cap := selfHealCap(ws)
	healKey := workspaceID + "#" + strconv.Itoa(issueNumber) + "#" + tfi.HeadSHA
	newAttempts := 1
	if v, loaded := a.healAttempts.LoadOrStore(healKey, 1); loaded {
		newAttempts = v.(int) + 1
		a.healAttempts.Store(healKey, newAttempts)
	}
	a.assembler.SetSelfHealAttempts(workspaceID, issueNumber, newAttempts, cap, newAttempts >= cap)
	return nil
}

// handlePRChecksFailed handles EvtPRChecksFailed: emits a toast and, if the
// workspace has auto_self_heal enabled and the cap is not reached, auto-spawns
// a Continue-Work session.
func (a *App) handlePRChecksFailed(e eventbus.Event) {
	if e.Type != eventbus.EvtPRChecksFailed {
		return
	}
	p, ok := e.Payload.(eventbus.PRChecksFailed)
	if !ok {
		return
	}
	title := toastWorkspaceName(a.wsReg, p.WorkspaceID)
	if !a.notificationsSuppressed() {
		a.emitToast("error", title,
			fmt.Sprintf("#%d TEST FAILURE — %d check(s) red on PR #%d", p.IssueNumber, len(p.FailingJobs), p.PRNumber),
			map[string]any{
				"workspace_id": p.WorkspaceID,
				"issue_number": p.IssueNumber,
				"action":       "focus_card",
			})
	}
	// Auto-spawn if workspace enables it and cap not reached.
	ws, ok := a.wsReg.Get(p.WorkspaceID)
	if !ok {
		return
	}
	if !autoSelfHealEnabled(ws) {
		return
	}
	cap := selfHealCap(ws)
	healKey := p.WorkspaceID + "#" + strconv.Itoa(p.IssueNumber) + "#" + p.HeadSHA
	curRaw, _ := a.healAttempts.Load(healKey)
	cur := 0
	if curRaw != nil {
		cur = curRaw.(int)
	}
	if cur >= cap {
		a.assembler.SetSelfHealAttempts(p.WorkspaceID, p.IssueNumber, cur, cap, true)
		return
	}
	// Spawn asynchronously — don't block the bus handler.
	go func() {
		if err := a.SelfHeal(p.WorkspaceID, p.IssueNumber); err != nil {
			log.Printf("auto self-heal #%d: %v", p.IssueNumber, err)
		}
	}()
}

// handlePRChecksRecovered handles EvtPRChecksRecovered: emits a toast when
// a previously-failing PR's checks go green.
func (a *App) handlePRChecksRecovered(e eventbus.Event) {
	if e.Type != eventbus.EvtPRChecksRecovered {
		return
	}
	p, ok := e.Payload.(eventbus.PRChecksRecovered)
	if !ok {
		return
	}
	// Clear attempt counter for this PR SHA now that it's green.
	healKey := p.WorkspaceID + "#" + strconv.Itoa(p.IssueNumber) + "#" + p.HeadSHA
	a.healAttempts.Delete(healKey)
	if !a.notificationsSuppressed() {
		title := toastWorkspaceName(a.wsReg, p.WorkspaceID)
		a.emitToast("success", title,
			fmt.Sprintf("#%d checks recovered on PR #%d — CI green", p.IssueNumber, p.PRNumber),
			map[string]any{
				"workspace_id": p.WorkspaceID,
				"issue_number": p.IssueNumber,
				"action":       "focus_card",
			})
	}
}

// handlePRConflictsDetected handles EvtPRConflictsDetected (#124): emits a toast
// so the user sees the red glow immediately even before the next frontend poll.
func (a *App) handlePRConflictsDetected(e eventbus.Event) {
	if e.Type != eventbus.EvtPRConflictsDetected {
		return
	}
	p, ok := e.Payload.(eventbus.PRConflictsDetected)
	if !ok {
		return
	}
	if !a.notificationsSuppressed() {
		title := toastWorkspaceName(a.wsReg, p.WorkspaceID)
		a.emitToast("error", title,
			fmt.Sprintf("#%d MERGE CONFLICT — PR #%d cannot be merged into %s", p.IssueNumber, p.PRNumber, p.Base),
			map[string]any{
				"workspace_id": p.WorkspaceID,
				"issue_number": p.IssueNumber,
				"action":       "focus_card",
			})
	}
}

// handlePRConflictsResolved handles EvtPRConflictsResolved (#124): emits a
// success toast when a previously-conflicted PR becomes mergeable again.
func (a *App) handlePRConflictsResolved(e eventbus.Event) {
	if e.Type != eventbus.EvtPRConflictsResolved {
		return
	}
	p, ok := e.Payload.(eventbus.PRConflictsResolved)
	if !ok {
		return
	}
	if !a.notificationsSuppressed() {
		title := toastWorkspaceName(a.wsReg, p.WorkspaceID)
		a.emitToast("success", title,
			fmt.Sprintf("#%d merge conflicts resolved — PR #%d is mergeable", p.IssueNumber, p.PRNumber),
			map[string]any{
				"workspace_id": p.WorkspaceID,
				"issue_number": p.IssueNumber,
				"action":       "focus_card",
			})
	}
}

// ResolveConflicts spawns a Continue-Work session pre-populated with conflict
// context from the most recent EvtPRConflictsDetected event (#124).
func (a *App) ResolveConflicts(workspaceID string, issueNumber int) error {
	if a.assembler == nil {
		return fmt.Errorf("assembler unavailable")
	}
	view, err := a.assembler.Assemble(workspaceID, issueNumber)
	if err != nil {
		return fmt.Errorf("assemble issue view: %w", err)
	}
	ci := view.ConflictsInfo
	if ci == nil {
		return fmt.Errorf("no merge conflict recorded for issue #%d", issueNumber)
	}
	note := handlers.BuildConflictResolveNote(ci)
	return a.ContinueWork(workspaceID, issueNumber, note)
}

// buildSelfHealNote constructs the Continue-Work note for the self-heal worker.
func (a *App) buildSelfHealNote(tfi *issueview.TestsFailingInfo) string {
	note := "CI self-heal: the following GitHub Actions checks are failing on the PR:\n\n"
	for i, job := range tfi.FailingJobs {
		note += fmt.Sprintf("- %s", job)
		if i < len(tfi.FailingCheckRunURLs) && tfi.FailingCheckRunURLs[i] != "" {
			note += fmt.Sprintf(" (%s)", tfi.FailingCheckRunURLs[i])
		}
		note += "\n"
	}
	note += "\nPlease investigate the failures, fix the root cause, and push a fixup commit."
	return note
}

// autoSelfHealEnabled reports whether the workspace has auto-self-heal on.
// Default is true (nil pointer → enabled).
func autoSelfHealEnabled(ws types.Workspace) bool {
	return ws.SkillProfile.AutoSelfHeal == nil || *ws.SkillProfile.AutoSelfHeal
}

// selfHealCap returns the configured attempt cap for the workspace (default 3).
func selfHealCap(ws types.Workspace) int {
	if ws.SkillProfile.SelfHealAttemptCap > 0 {
		return ws.SkillProfile.SelfHealAttemptCap
	}
	return 3
}

// hasActiveExecuteSession returns true if an execute-mode worker is currently
// running (or waiting / blocked) for the given issue.
func (a *App) hasActiveExecuteSession(workspaceID string, number int) bool {
	if a.mgr == nil {
		return false
	}
	for _, s := range a.mgr.Snapshot() {
		if s.WorkspaceID != workspaceID || s.IssueNumber != number {
			continue
		}
		if s.Mode != types.ModeExecute {
			continue
		}
		switch s.State {
		case types.StateRunning, types.StateWaitingForInput, types.StateBlocked:
			return true
		}
	}
	return false
}

// FetchPRChecks runs `gh pr checks <prNumber>` in the workspace repo and
// returns the combined output. Used by the ContinueModal to pre-fill the note
// textarea with failing check output (q1=A: pre-fill automatically).
func (a *App) FetchPRChecks(workspaceID string, prNumber int) (string, error) {
	ws, ok := a.wsReg.Get(workspaceID)
	if !ok {
		return "", fmt.Errorf("unknown workspace %q", workspaceID)
	}
	cmd := exec.Command("gh", "pr", "checks", strconv.Itoa(prNumber), "--fail-fast")
	cmd.Dir = ws.RepoPath
	out, _ := cmd.CombinedOutput()
	// gh pr checks exits non-zero when checks are failing — that's exactly the
	// case we want to surface, so ignore the error and return the output.
	return string(out), nil
}

// RejectPlan moves the card back to TODO and frees the worker slot.
// (No worker to kill if plan mode finished — but if a revision is in flight,
// caller is expected to KillSession first.)
func (a *App) RejectPlan(workspaceID string, issueNumber int) error {
	if a.store == nil {
		return fmt.Errorf("store unavailable")
	}
	if err := a.store.MoveIssueColumn(workspaceID, issueNumber, types.ColTodo); err != nil {
		return err
	}
	a.bus.Publish(eventbus.EvtPlanRejected, map[string]any{
		"workspace_id": workspaceID,
		"issue_number": issueNumber,
	})
	return nil
}

// --- Orchestrator pool resolver (issue #39) ---

// resolveOrchestratorLLM returns the orchestrator's LLM client for the next
// rank pass, or nil when no enabled role=orchestrator pool exists. Resolved
// on every runRank call so changes via the UI take effect on the next event
// tick without an app restart.
func (a *App) resolveOrchestratorLLM() orchestrator.LLM {
	if a.poolReg == nil || a.providers == nil {
		return nil
	}
	pool, ok := a.poolReg.OrchestratorPool()
	if !ok {
		return nil
	}
	prov, ok := a.providers.Get(pool.Provider)
	if !ok {
		return nil
	}
	return &orchestratorChatLLM{provider: prov, pool: pool}
}

// orchestratorChatLLM adapts a llm.Provider into the orchestrator.LLM
// interface (Generate(system,user) -> string). Each Generate call is a
// one-shot HTTP request through the provider's ChatJSON path.
type orchestratorChatLLM struct {
	provider llm.Provider
	pool     types.Pool
}

func (o *orchestratorChatLLM) Generate(ctx context.Context, system, prompt string) (string, error) {
	return o.provider.ChatJSON(ctx, o.pool, system, prompt)
}

// migrateOrchestratorPool moves the legacy ollama_url / ollama_model settings
// into a role=orchestrator pool the first time #39 is run. Idempotent: no-op
// if a role=orchestrator pool already exists, and no-op on fresh installs
// (where the legacy keys were never written).
func (a *App) migrateOrchestratorPool() error {
	if a.store == nil {
		return nil
	}
	rows, err := a.store.ListPoolsByRole(types.RoleOrchestrator)
	if err != nil {
		return err
	}
	if len(rows) > 0 {
		_ = a.store.DeleteSetting("ollama_url")
		_ = a.store.DeleteSetting("ollama_model")
		return nil
	}
	url, _ := a.store.GetSetting("ollama_url")
	model, _ := a.store.GetSetting("ollama_model")
	if url == "" && model == "" {
		return nil
	}
	provider := types.ProviderOllama
	if strings.Contains(url, ":1234") || strings.Contains(strings.ToLower(url), "lmstudio") {
		provider = types.ProviderLMStudio
	}
	name := "orchestrator"
	if model != "" {
		name = "orchestrator (" + model + ")"
	}
	pool := types.Pool{
		ID:        uuid.NewString(),
		Name:      name,
		Provider:  provider,
		Endpoint:  url,
		Model:     model,
		Capacity:  1,
		Enabled:   true,
		Role:      types.RoleOrchestrator,
		CreatedAt: time.Now(),
	}
	if err := a.store.SavePool(pool); err != nil {
		return err
	}
	_ = a.store.DeleteSetting("ollama_url")
	_ = a.store.DeleteSetting("ollama_model")
	return nil
}

// RunOrchestrator triggers an immediate rank pass against the active goal.
func (a *App) RunOrchestrator() error { return a.orch.RunNow() }

// FilterIssuesByActiveGoal applies the active goal's IssueQuery to the given
// issue list. Returns the unfiltered list if no active goal.
func (a *App) FilterIssuesByActiveGoal(issues []types.Issue) []types.Issue {
	if a.store == nil {
		return issues
	}
	goals, err := a.store.ListGoals()
	if err != nil {
		return issues
	}
	for _, g := range goals {
		if g.Status == types.GoalActive {
			return goalfilter.Apply(g.IssueFilter, issues)
		}
	}
	return issues
}

// SetGoalStatus moves a goal to a non-active status (backlog/achieved/abandoned).
func (a *App) SetGoalStatus(id, status string) error {
	if a.store == nil {
		return fmt.Errorf("store unavailable")
	}
	gs := types.GoalStatus(status)
	if err := a.store.SetGoalStatus(id, gs); err != nil {
		return err
	}
	if gs == types.GoalAchieved {
		now := time.Now()
		if g, err := a.store.GetGoal(id); err == nil {
			g.AchievedAt = &now
			_ = a.store.SaveGoal(g)
		}
	}
	a.bus.Publish(eventbus.EvtGoalUpdated, id)
	return nil
}

// acquirePlanPool reserves a slot on a role=plan pool and returns the
// resolved row. Callers MUST poolReg.ReleaseByPool(pool.ID) on spawn failure.
// Strict role=plan — never falls back to role=work (issue #39 rev2).
func (a *App) acquirePlanPool(ws types.Workspace) (types.Pool, bool) {
	return a.acquirePool(func() (string, bool) { return a.poolReg.AcquireForPlan(ws) })
}

// acquireWorkPool reserves a slot on a role=work pool. Strict — never falls
// back to role=plan.
func (a *App) acquireWorkPool(ws types.Workspace) (types.Pool, bool) {
	return a.acquirePool(func() (string, bool) { return a.poolReg.AcquireForWork(ws) })
}

func (a *App) acquirePool(reserve func() (string, bool)) (types.Pool, bool) {
	if a.poolReg == nil || a.store == nil {
		return types.Pool{}, false
	}
	id, ok := reserve()
	if !ok {
		return types.Pool{}, false
	}
	pool, err := a.store.GetPool(id)
	if err != nil {
		a.poolReg.ReleaseByPool(id)
		log.Printf("acquirePool: GetPool(%s): %v", id, err)
		return types.Pool{}, false
	}
	return pool, true
}

// reconcilePoolActiveCounters re-derives every pool's in-memory `active`
// counter from the authoritative DB count of running sessions. Called at
// startup AND on every terminal session-state-changed / worker-slot-freed
// event so any acquire-without-matching-release leak self-corrects within
// one event tick.
//
// Without this, the in-memory counter is a hand-maintained ledger that
// drifts whenever ANY spawn-error path forgets to release. Witnessed live
// as Planners 1/2 with zero plan sessions actually running. The "Reset
// counters" UI button was the only fix; now drift heals automatically.
func (a *App) reconcilePoolActiveCounters(reason string) {
	if a.poolReg == nil || a.store == nil {
		return
	}
	counts, err := a.store.CountRunningSessionsByPool()
	if err != nil {
		log.Printf("reconcile pool counters (reason=%s): %v", reason, err)
		return
	}
	a.poolReg.ReconcileActive(counts)
}

// SpawnPlanForIssue spawns a plan-mode worker for the given issue in the given workspace.
// Triggered by drag-to-PLAN once the board is wired (issue #3) — exposed now so flows can be tested.
func (a *App) SpawnPlanForIssue(workspaceID string, issueNumber int) (*types.Session, error) {
	if a.wsReg == nil {
		return nil, fmt.Errorf("workspace registry unavailable")
	}
	ws, ok := a.wsReg.Get(workspaceID)
	if !ok {
		return nil, fmt.Errorf("unknown workspace %q", workspaceID)
	}
	pool, ok := a.acquirePlanPool(ws)
	if !ok {
		return nil, fmt.Errorf("no plan pool available")
	}
	sess, err := a.mgr.SpawnPlan(ws, types.Issue{Number: issueNumber, WorkspaceID: workspaceID}, pool)
	if err != nil {
		a.poolReg.ReleaseByPool(pool.ID)
		return nil, err
	}
	return sess, nil
}

// ListSessions returns the live in-process sessions plus the most-recent
// terminal session per (workspace, issue) so the frontend's lastFailure
// selector can render the red-glow / blocked-reason overlay on cards whose
// failure happened before the conductor restarted. Without this rehydration,
// post-restart the card shows a blank in-progress state with no indication
// that the prior execute died — every restart silently swallows the failure
// context.
func (a *App) ListSessions() []types.Session {
	if a.mgr == nil {
		return nil
	}
	live := a.mgr.Snapshot()
	if a.store == nil {
		return live
	}
	terminal, err := a.store.RecentTerminalSessions(50)
	if err != nil {
		log.Printf("ListSessions: RecentTerminalSessions: %v", err)
		return live
	}
	// De-dup by id: live wins over terminal (live is current truth).
	seen := make(map[string]struct{}, len(live)+len(terminal))
	out := make([]types.Session, 0, len(live)+len(terminal))
	for _, s := range live {
		seen[s.ID] = struct{}{}
		out = append(out, s)
	}
	for _, s := range terminal {
		if _, dup := seen[s.ID]; dup {
			continue
		}
		out = append(out, s)
	}
	return out
}

// ReadTranscript returns the full transcript file for a session id.
// Used by the SessionDrawer when re-attaching to a session whose PTY is no
// longer streaming (e.g., after app restart).
func (a *App) ReadTranscript(sessionID string) (string, error) {
	if a.store == nil {
		return "", fmt.Errorf("store unavailable")
	}
	path, err := a.store.SessionTranscriptPath(sessionID)
	if err != nil {
		return "", err
	}
	if path == "" {
		return "", nil
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	return string(b), nil
}

// KillSession terminates a running session.
func (a *App) KillSession(id string) error { return a.mgr.Kill(id) }

// CancelSession gracefully stops a running session: sends SIGTERM and force-
// kills after 3 seconds if the process hasn't exited. For harness sessions
// the context cancel is sufficient (issue #99).
func (a *App) CancelSession(id string) error { return a.mgr.KillGraceful(id) }

// SendInput writes user input to a PTY session.
func (a *App) SendInput(id, text string) error { return a.mgr.SendInput(id, text) }

// --- GitHub login (OAuth Device Flow) ---

// GitHubAuthStatus returns the current login status. If a token is cached,
// the user object is fetched and returned.
func (a *App) GitHubAuthStatus() (*githubauth.User, error) {
	t, err := githubauth.LoadToken(a.cfgDir)
	if err != nil || t == nil {
		return nil, err
	}
	return githubauth.FetchUser(a.ctx, t.AccessToken)
}

// GitHubLoginStart kicks off device flow and opens the verification URL in the browser.
// Returns the user_code so the UI can display it. Frontend should then call GitHubLoginPoll.
func (a *App) GitHubLoginStart() (*githubauth.DeviceCode, error) {
	dc, err := a.auth.RequestDeviceCode(a.ctx)
	if err != nil {
		return nil, err
	}
	a.pendingDevice = dc
	wruntime.BrowserOpenURL(a.ctx, dc.VerificationURI)
	return dc, nil
}

// GitHubLoginPoll blocks until the user authorizes (or the code expires).
// Returns the authenticated user on success.
func (a *App) GitHubLoginPoll() (*githubauth.User, error) {
	if a.pendingDevice == nil {
		return nil, fmt.Errorf("no login in progress; call GitHubLoginStart first")
	}
	tok, err := a.auth.PollForToken(a.ctx, a.pendingDevice)
	a.pendingDevice = nil
	if err != nil {
		return nil, err
	}
	if err := githubauth.SaveToken(a.cfgDir, tok); err != nil {
		return nil, err
	}
	return githubauth.FetchUser(a.ctx, tok.AccessToken)
}

// GitHubLogout clears the cached token.
func (a *App) GitHubLogout() error { return githubauth.ClearToken(a.cfgDir) }

// markSessionBlocked retroactively flips a session row to BLOCKED with the
// given reason and re-publishes the state change so the card surfaces the
// failure (issue #67). Used by post-completion handlers (plan ingest, plan
// save, …) where the session is already `completed` from the worker's POV
// but the conductor's downstream processing failed silently. Without this,
// the card looks "completed" while no plan exists and the user has no
// indication anything went wrong.
func (a *App) markSessionBlocked(sess types.Session, reason string) {
	if a.store == nil {
		return
	}
	if len(reason) > 500 {
		reason = reason[:500]
	}
	prev := sess.State
	sess.State = types.StateBlocked
	sess.BlockedReason = reason
	if err := a.store.SaveSession(&sess, sess.Transcript); err != nil {
		log.Printf("markSessionBlocked: SaveSession %s: %v", sess.ID, err)
		return
	}
	if err := a.store.UpdateSessionState(sess.ID, types.StateBlocked); err != nil {
		log.Printf("markSessionBlocked: UpdateSessionState %s: %v", sess.ID, err)
	}
	a.handleSessionStateChange(sess, prev)
}

// GitHubListRepos returns repos the authenticated user can access. Used by the
// "Add Workspace" picker so the user doesn't have to type a path.
func (a *App) GitHubListRepos() ([]*gh.Repository, error) {
	t, err := githubauth.LoadToken(a.cfgDir)
	if err != nil || t == nil {
		return nil, fmt.Errorf("not logged in")
	}
	return githubauth.FetchRepos(a.ctx, t.AccessToken)
}

func configDir() (string, error) {
	if d := os.Getenv("PRISMCONDUCTOR_DATA_DIR"); d != "" {
		return d, nil
	}
	switch runtime.GOOS {
	case "darwin":
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		return filepath.Join(home, "Library", "Application Support", "PrismConductor"), nil
	case "windows":
		return filepath.Join(os.Getenv("APPDATA"), "PrismConductor"), nil
	default:
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		return filepath.Join(home, ".config", "prismconductor"), nil
	}
}

// GetPoolUsage returns the last-seen rate-limit snapshot for every pool that
// has reported usage. Called by the frontend GoalRowStats component.
func (a *App) GetPoolUsage() ([]types.PoolUsage, error) {
	if a.store == nil {
		return nil, nil
	}
	return a.store.ListPoolUsage()
}

// DiagnoseIssue walks a small decision tree of conductor invariants and returns
// a plain-English explanation of the card's current state plus a suggested
// next action. All checks are local and synchronous; no LLM calls are made.
func (a *App) DiagnoseIssue(workspaceID string, issueNumber int) (*diagnose.IssueDiagnosis, error) {
	if a.wsReg == nil {
		return nil, fmt.Errorf("workspace registry unavailable")
	}
	ws, ok := a.wsReg.Get(workspaceID)
	if !ok {
		return nil, fmt.Errorf("workspace %q not found", workspaceID)
	}

	issue, ok := a.findIssue(workspaceID, issueNumber)
	if !ok {
		return nil, fmt.Errorf("issue #%d not found", issueNumber)
	}

	// Gather persisted sessions newest-first.
	var sessions []types.Session
	if a.store != nil {
		sessions, _ = a.store.ListSessionsForIssue(workspaceID, issueNumber)
	}

	// Find any live in-memory session.
	var liveSession *types.Session
	if a.mgr != nil {
		for _, s := range a.mgr.Snapshot() {
			if s.WorkspaceID == workspaceID && s.IssueNumber == issueNumber {
				cp := s
				liveSession = &cp
				break
			}
		}
	}

	// Resolve pool metadata for the relevant session (live first, then last persisted).
	poolActive, poolCap, poolName := 0, 0, ""
	poolID := ""
	if liveSession != nil {
		poolID = liveSession.PoolID
	} else if len(sessions) > 0 {
		poolID = sessions[0].PoolID
	}
	if poolID != "" && a.poolReg != nil {
		poolActive = a.poolReg.ActiveCount(poolID)
		for _, ps := range a.poolReg.Snapshot() {
			if ps.Pool.ID == poolID {
				poolCap = ps.Pool.Capacity
				poolName = ps.Pool.Name
				break
			}
		}
	}

	// Check pending pool queue.
	pendingPool := false
	if a.store != nil {
		if pending, err := a.store.ListPendingPools(200); err == nil {
			for _, req := range pending {
				if req.WorkspaceID == workspaceID && req.IssueNumber == issueNumber {
					pendingPool = true
					break
				}
			}
		}
	}

	// Check worktree on disk.
	worktreeDir := filepath.Join(ws.RepoPath, ".prismconductor", "worktrees",
		fmt.Sprintf("%s-%d", workspaceID, issueNumber))
	_, werr := os.Stat(worktreeDir)
	worktreeOnDisk := werr == nil

	// Check whether an answer file for a paused mid-run question exists.
	answerPending := false
	checkPendingQID := func(qid string) {
		if qid == "" {
			return
		}
		aPath := filepath.Join(ws.RepoPath, ".prismconductor", "answers", qid+".json")
		if _, err := os.Stat(aPath); err == nil {
			answerPending = true
		}
	}
	if liveSession != nil {
		checkPendingQID(liveSession.PendingQuestionID)
	} else if len(sessions) > 0 {
		checkPendingQID(sessions[0].PendingQuestionID)
	}

	// Check plan file on disk vs. in DB.
	planOnDisk, planRevision := false, 0
	if a.store != nil {
		if latestPlan, err := a.store.LatestPlan(workspaceID, issueNumber); err == nil && latestPlan != nil {
			planRevision = latestPlan.Revision
			pPath := planio.PlanPath(ws.RepoPath, issueNumber, latestPlan.Revision)
			if _, err2 := os.Stat(pPath); err2 == nil {
				planOnDisk = true
			}
		}
	}

	snap := diagnose.Snapshot{
		Issue:          issue,
		Sessions:       sessions,
		LiveSession:    liveSession,
		PoolActive:     poolActive,
		PoolCapacity:   poolCap,
		PoolName:       poolName,
		PendingPool:    pendingPool,
		WorktreeOnDisk: worktreeOnDisk,
		AnswerPending:  answerPending,
		PlanOnDisk:     planOnDisk,
		PlanRevision:   planRevision,
	}
	result := diagnose.Run(snap)
	return &result, nil
}

// --- Issue #101: cost / token visibility helpers ---

// PoolSpendToday returns the estimated spend in USD for the given pool since
// midnight UTC. Returns 0 on error or when no data is available.
func (a *App) PoolSpendToday(poolID string) float64 {
	if a.store == nil {
		return 0
	}
	since := todayUTC()
	cents, err := a.store.PoolSpendCents(poolID, since)
	if err != nil {
		log.Printf("PoolSpendToday %s: %v", poolID, err)
		return 0
	}
	return cents / 100
}

// PoolSpendThisWeek returns the estimated spend in USD for the given pool
// since the start of the current ISO week (Monday 00:00 UTC).
func (a *App) PoolSpendThisWeek(poolID string) float64 {
	if a.store == nil {
		return 0
	}
	since := thisWeekUTC()
	cents, err := a.store.PoolSpendCents(poolID, since)
	if err != nil {
		log.Printf("PoolSpendThisWeek %s: %v", poolID, err)
		return 0
	}
	return cents / 100
}

// WorkspaceSpendToday returns the estimated spend in USD across all sessions
// for the given workspace since midnight UTC.
func (a *App) WorkspaceSpendToday(workspaceID string) float64 {
	if a.store == nil {
		return 0
	}
	cents, err := a.store.WorkspaceSpendCents(workspaceID, todayUTC())
	if err != nil {
		log.Printf("WorkspaceSpendToday %s: %v", workspaceID, err)
		return 0
	}
	return cents / 100
}

// GoalSpendResult is the per-goal spend summary returned by GoalSpendToday.
type GoalSpendResult struct {
	TotalUSD float64 `json:"total_usd"`
	RunCount int     `json:"run_count"`
}

// GoalSpendToday returns the estimated spend in USD and the number of terminal
// sessions for the given goal since midnight UTC.
func (a *App) GoalSpendToday(goalID string) GoalSpendResult {
	if a.store == nil {
		return GoalSpendResult{}
	}
	cents, count, err := a.store.GoalSpendCents(goalID, todayUTC())
	if err != nil {
		log.Printf("GoalSpendToday %s: %v", goalID, err)
		return GoalSpendResult{}
	}
	return GoalSpendResult{TotalUSD: cents / 100, RunCount: count}
}

// SpawnEstimate holds a pre-flight cost estimate for a plan-approve spawn.
type SpawnEstimate struct {
	Tokens    int64   `json:"tokens"`
	CostCents float64 `json:"cost_cents"`
	Model     string  `json:"model"`
}

// EstimateSpawnCost returns a cheap pre-flight token and cost estimate for an
// execute spawn (issue #101, Q3=yes). Uses the chars/4 heuristic against the
// pool's model pricing. Returns zero values when pool or pricing is unknown.
func (a *App) EstimateSpawnCost(workspaceID string, issueNumber int, poolID string) SpawnEstimate {
	if a.store == nil || a.poolReg == nil {
		return SpawnEstimate{}
	}
	pool, err := a.store.GetPool(poolID)
	if err != nil {
		return SpawnEstimate{}
	}
	issue, err := a.store.LoadIssue(workspaceID, issueNumber)
	if err != nil {
		return SpawnEstimate{}
	}
	plan, _ := a.store.LatestPlan(workspaceID, issueNumber)
	promptText := issue.Body
	if plan != nil {
		promptText += "\n" + plan.PlanMarkdown
	}
	tokens, costCents := llm.EstimatePromptCost(promptText, pool.Model)
	return SpawnEstimate{Tokens: tokens, CostCents: costCents, Model: pool.Model}
}

// PostPRComment posts a comment to the GitHub PR thread for a REVIEW-column
// issue. When requestFix is true it also spawns a Continue Work session so
// the worker addresses the feedback. Issue #159.
func (a *App) PostPRComment(workspaceID string, issueNumber int, body string, requestFix bool) error {
	if a.wsReg == nil || a.store == nil || a.gh == nil {
		return fmt.Errorf("registry/store/github unavailable")
	}
	if strings.TrimSpace(body) == "" {
		return fmt.Errorf("comment body is required")
	}
	ws, ok := a.wsReg.Get(workspaceID)
	if !ok {
		return fmt.Errorf("unknown workspace %q", workspaceID)
	}
	iss, err := a.store.LoadIssue(workspaceID, issueNumber)
	if err != nil {
		return fmt.Errorf("load issue: %w", err)
	}
	if iss.PRNumber == nil {
		return fmt.Errorf("issue #%d has no associated PR", issueNumber)
	}

	// Post to GitHub.
	commentID, err := a.gh.PostIssueComment(a.ctx, ws, issueNumber, body)
	if err != nil {
		// Queue as pending_post so the UI shows a pending indicator; clear on next success.
		_, _ = a.store.UpsertPRComment(types.PRComment{
			WorkspaceID: workspaceID,
			IssueNumber: issueNumber,
			CommentID:   -time.Now().UnixNano(), // temporary negative ID to avoid clash
			Author:      "me",
			Body:        body,
			Kind:        types.PRCommentKindConversation,
			CreatedAt:   time.Now(),
			PendingPost: true,
		})
		return fmt.Errorf("post comment: %w", err)
	}

	// Record in local store (no pending_post since it succeeded).
	_, _ = a.store.UpsertPRComment(types.PRComment{
		WorkspaceID: workspaceID,
		IssueNumber: issueNumber,
		CommentID:   commentID,
		Author:      "me",
		Body:        body,
		Kind:        types.PRCommentKindConversation,
		CreatedAt:   time.Now(),
	})
	if a.bus != nil {
		a.bus.Publish(eventbus.EvtPRCommentPosted, map[string]any{
			"workspace_id": workspaceID,
			"issue_number": issueNumber,
			"comment_id":   commentID,
		})
	}

	if !requestFix {
		return nil
	}

	// Auto-continue: workspace setting auto_continue_on_comment defaults to true.
	autoFix := ws.SkillProfile.AutoContinueOnComment == nil || *ws.SkillProfile.AutoContinueOnComment
	if !autoFix {
		// Caller (frontend) must show its own confirmation dialog; we don't
		// spawn here. The request is fulfilled by a separate RequestFixForComments call.
		return nil
	}
	note := fmt.Sprintf("PR comment feedback — please address the following:\n\n%s", body)
	return a.ContinueWork(workspaceID, issueNumber, note)
}

// AcknowledgeComment marks a single PR comment as read. Issue #159.
func (a *App) AcknowledgeComment(workspaceID string, issueNumber int, commentID int64) error {
	if a.store == nil {
		return fmt.Errorf("store unavailable")
	}
	if err := a.store.MarkPRCommentRead(workspaceID, issueNumber, commentID); err != nil {
		return err
	}
	if a.assembler != nil {
		a.assembler.Reassemble(workspaceID, issueNumber)
	}
	return nil
}

// RequestFixForComments marks the selected comments as read, builds a
// consolidated note, and spawns a Continue Work session. Issue #159.
func (a *App) RequestFixForComments(workspaceID string, issueNumber int, commentIDs []int64) error {
	if a.store == nil {
		return fmt.Errorf("store unavailable")
	}
	if len(commentIDs) == 0 {
		return fmt.Errorf("no comments selected")
	}

	// Fetch the comment bodies to build the task note.
	all, err := a.store.ListPRComments(workspaceID, issueNumber)
	if err != nil {
		return fmt.Errorf("list pr comments: %w", err)
	}
	idSet := make(map[int64]bool, len(commentIDs))
	for _, id := range commentIDs {
		idSet[id] = true
	}

	var noteLines []string
	for _, c := range all {
		if !idSet[c.CommentID] {
			continue
		}
		entry := fmt.Sprintf("[%s] %s: %s", c.Kind, c.Author, c.Body)
		if c.FilePath != "" {
			entry = fmt.Sprintf("[%s:%d] %s: %s", c.FilePath, c.LineNumber, c.Author, c.Body)
		}
		noteLines = append(noteLines, entry)
		_ = a.store.MarkPRCommentRead(workspaceID, issueNumber, c.CommentID)
	}
	if len(noteLines) == 0 {
		return fmt.Errorf("selected comments not found")
	}

	note := "PR review feedback — please address in order:\n\n" + strings.Join(noteLines, "\n\n")
	if err := a.ContinueWork(workspaceID, issueNumber, note); err != nil {
		return err
	}
	if a.assembler != nil {
		a.assembler.Reassemble(workspaceID, issueNumber)
	}
	return nil
}

// ListPRComments returns all stored PR comments for an issue (read + unread),
// ordered oldest-first. Issue #159.
func (a *App) ListPRComments(workspaceID string, issueNumber int) ([]types.PRComment, error) {
	if a.store == nil {
		return nil, fmt.Errorf("store unavailable")
	}
	return a.store.ListPRComments(workspaceID, issueNumber)
}

func todayUTC() time.Time {
	now := time.Now().UTC()
	return time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)
}

func thisWeekUTC() time.Time {
	now := time.Now().UTC()
	weekday := int(now.Weekday())
	if weekday == 0 {
		weekday = 7 // Sunday → 7 so Monday is day 1
	}
	monday := now.AddDate(0, 0, -(weekday - 1))
	return time.Date(monday.Year(), monday.Month(), monday.Day(), 0, 0, 0, 0, time.UTC)
}

// --- Agent terminal (issue #161) ---

func (a *App) emitAgentData(workspaceID, dataB64 string) {
	wruntime.EventsEmit(a.ctx, "agentterm.output", map[string]string{
		"workspace_id": workspaceID,
		"data":         dataB64,
	})
}

func (a *App) emitAgentExit(workspaceID string, exitCode int) {
	wruntime.EventsEmit(a.ctx, "agentterm.exit", map[string]any{
		"workspace_id": workspaceID,
		"exit_code":    exitCode,
	})
}

// ListAvailableAgents returns agent CLI binaries found on PATH.
func (a *App) ListAvailableAgents() []types.AgentInfo {
	return agentterm.DiscoverAgents()
}

// StartAgentSession spawns an ephemeral PTY-backed agent for the workspace.
// Any existing session for that workspace is killed first.
//
// Resolves the workspace's RepoPath as the subprocess cwd. Without this,
// the agent inherits the conductor's own cwd (on macOS Wails: typically
// `/` because Finder-launched .app bundles start there), so `claude` /
// `aider` / etc. open with `pwd` pointing at the wrong directory and
// can't see the user's repo until they manually `cd`. We refuse to spawn
// without a resolvable workspace — the agent panel is workspace-scoped.
func (a *App) StartAgentSession(workspaceID, agentBin string, args []string, cols, rows uint16) (*types.AgentTermSession, error) {
	if a.agentTerm == nil {
		return nil, fmt.Errorf("agent terminal manager unavailable")
	}
	if a.wsReg == nil {
		return nil, fmt.Errorf("workspace registry unavailable")
	}
	ws, ok := a.wsReg.Get(workspaceID)
	if !ok {
		return nil, fmt.Errorf("unknown workspace %q", workspaceID)
	}
	cwd := ws.RepoPath
	if cwd == "" {
		return nil, fmt.Errorf("workspace %q has no repo_path configured", workspaceID)
	}
	pid, err := a.agentTerm.Start(workspaceID, agentBin, args, cwd, cols, rows)
	if err != nil {
		return nil, err
	}
	return &types.AgentTermSession{
		WorkspaceID: workspaceID,
		SessionID:   uuid.NewString(),
		AgentBin:    agentBin,
		PID:         pid,
		Cwd:         cwd,
	}, nil
}

// WriteAgentInput sends raw bytes (base64-encoded) to the workspace's PTY.
func (a *App) WriteAgentInput(workspaceID, dataB64 string) error {
	if a.agentTerm == nil {
		return fmt.Errorf("agent terminal manager unavailable")
	}
	return a.agentTerm.Write(workspaceID, dataB64)
}

// ResizeAgentTerm resizes the PTY window for the workspace's active session.
func (a *App) ResizeAgentTerm(workspaceID string, cols, rows uint16) error {
	if a.agentTerm == nil {
		return fmt.Errorf("agent terminal manager unavailable")
	}
	return a.agentTerm.Resize(workspaceID, cols, rows)
}

// KillAgentSession terminates the active agent session for the workspace.
func (a *App) KillAgentSession(workspaceID string) error {
	if a.agentTerm == nil {
		return fmt.Errorf("agent terminal manager unavailable")
	}
	return a.agentTerm.Kill(workspaceID)
}
