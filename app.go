package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"time"

	wruntime "github.com/wailsapp/wails/v2/pkg/runtime"

	"github.com/google/uuid"
	gh "github.com/google/go-github/v62/github"

	"prismconductor/internal/eventbus"
	"prismconductor/internal/goalfilter"
	"prismconductor/internal/githubauth"
	"prismconductor/internal/notify"
	"prismconductor/internal/ollama"
	"prismconductor/internal/orchestrator"
	"prismconductor/internal/session"
	"prismconductor/internal/skills/bundle"
	"prismconductor/internal/store"
	"prismconductor/internal/types"
	"prismconductor/internal/workerpool"
	"prismconductor/internal/workspace"
)

// App is the Wails-bound root. Methods are exposed to the frontend.
type App struct {
	ctx context.Context

	bus     *eventbus.Bus
	store   *store.Store
	mgr     *session.Manager
	pool    *workerpool.Pool
	llm     *ollama.Client
	orch    *orchestrator.Orchestrator
	wsReg   *workspace.Registry
	auth    *githubauth.Client
	cfgDir  string

	pendingDevice *githubauth.DeviceCode

	notifyMu      sync.Mutex
	lastNotifyKey string
	lastNotifyAt  int64
}

func NewApp() *App { return &App{} }

func (a *App) startup(ctx context.Context) {
	a.ctx = ctx

	cfgDir, _ := configDir()
	a.cfgDir = cfgDir
	bundleDir := filepath.Join(cfgDir, "skills")
	if _, err := bundle.Extract(bundleDir); err != nil {
		fmt.Fprintf(os.Stderr, "skill bundle extract: %v\n", err)
	}

	a.bus = eventbus.New()
	a.pool = workerpool.New(2)
	a.llm = ollama.New("", "")
	a.orch = orchestrator.New(a.bus, a.llm)
	a.auth = githubauth.New("")

	if s, err := store.Open(cfgDir); err != nil {
		fmt.Fprintf(os.Stderr, "store open: %v\n", err)
	} else {
		a.store = s
		_ = a.store.EnsureDepCacheTable()
		// Apply persisted Ollama settings if present.
		if u, _ := a.store.GetSetting("ollama_url"); u != "" {
			a.llm.URL = u
		}
		if m, _ := a.store.GetSetting("ollama_model"); m != "" {
			a.llm.Model = m
		}
	}
	if r, err := workspace.New(cfgDir); err != nil {
		fmt.Fprintf(os.Stderr, "workspace registry: %v\n", err)
	} else {
		a.wsReg = r
	}

	a.mgr = session.NewManager(a.bus, a.emitLine)
	a.mgr.Configure(filepath.Join(cfgDir, "transcripts"), a.store, a.handleSessionStateChange)

	// Hook the orchestrator up to the store now that both exist.
	if a.store != nil {
		a.orch.SetStore(a.store)

		// Persist every event for debugging + Phase 7 transcript pattern detector.
		a.bus.Subscribe(func(e eventbus.Event) {
			_ = a.store.LogEvent(string(e.Type), e.Payload)
			wruntime.EventsEmit(a.ctx, "bus."+string(e.Type), e.Payload)
		})
	}

	// Re-attach to sessions that were live at last shutdown (§15.3).
	if a.store != nil {
		if running, _, err := a.store.LoadRunningSessions(); err == nil {
			a.mgr.Reattach(running)
		}
	}
}

// handleSessionStateChange runs on every session state transition. Fans the
// new state to the UI as a Wails event and fires an OS notification per §15.6.
func (a *App) handleSessionStateChange(sess types.Session, prev types.SessionState) {
	wruntime.EventsEmit(a.ctx, "session.state", sess)

	if prev == sess.State {
		return
	}
	switch sess.State {
	case types.StateWaitingForInput, types.StateBlocked, types.StateCompleted, types.StateFailed:
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

		title := titleForWorkspace(a.wsReg, sess.WorkspaceID)
		body := notifyBody(sess)
		_ = notify.Send(title, body)
	}
}

func titleForWorkspace(reg *workspace.Registry, id string) string {
	if reg == nil || id == "" {
		return "PrismConductor"
	}
	if ws, ok := reg.Get(id); ok {
		return "PrismConductor — " + ws.DisplayName
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

// AddWorkspace registers a new workspace.
func (a *App) AddWorkspace(ws types.Workspace) error {
	if a.wsReg == nil {
		return fmt.Errorf("workspace registry unavailable")
	}
	if !ws.Enabled {
		ws.Enabled = true
	}
	return a.wsReg.Add(ws)
}

// UpdateWorkspace replaces a workspace's record.
func (a *App) UpdateWorkspace(ws types.Workspace) error {
	if a.wsReg == nil {
		return fmt.Errorf("workspace registry unavailable")
	}
	return a.wsReg.Update(ws)
}

// RemoveWorkspace removes a workspace by ID.
func (a *App) RemoveWorkspace(id string) error {
	if a.wsReg == nil {
		return fmt.Errorf("workspace registry unavailable")
	}
	return a.wsReg.Remove(id)
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

// ListIssues returns all issues, optionally filtered by workspace ID.
// Empty workspaceID = all workspaces.
func (a *App) ListIssues(workspaceID string) ([]types.Issue, error) {
	if a.store == nil {
		return nil, fmt.Errorf("store unavailable")
	}
	return a.store.ListIssues(workspaceID)
}

// MoveIssueColumn moves an issue card between columns. Emits EvtCardMovedManually
// per §15.5 if the move was triggered by the user (which it always is at this layer —
// orchestrator-driven moves bypass this method by writing the column directly).
func (a *App) MoveIssueColumn(workspaceID string, number int, column string) error {
	if a.store == nil {
		return fmt.Errorf("store unavailable")
	}
	if err := a.store.MoveIssueColumn(workspaceID, number, types.BoardColumn(column)); err != nil {
		return err
	}
	a.bus.Publish(eventbus.EvtCardMovedManually, map[string]any{
		"workspace_id": workspaceID,
		"number":       number,
		"column":       column,
	})
	return nil
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
	if err := a.store.SaveIssue(iss); err != nil {
		return nil, err
	}
	a.bus.Publish(eventbus.EvtIssueAdded, map[string]any{
		"workspace_id": workspaceID,
		"number":       number,
	})
	return &iss, nil
}

// --- Orchestrator + Ollama ---

// OllamaConfig is the user-visible Ollama settings.
type OllamaConfig struct {
	URL       string `json:"url"`
	Model     string `json:"model"`
	Available bool   `json:"available"`
}

// GetOllamaConfig returns the current settings + a presence check against the
// configured endpoint.
func (a *App) GetOllamaConfig() OllamaConfig {
	cfg := OllamaConfig{URL: a.llm.URL, Model: a.llm.Model}
	if ok, err := a.llm.Available(a.ctx); err == nil {
		cfg.Available = ok
	}
	return cfg
}

// SetOllamaConfig persists the user's URL/model picks.
func (a *App) SetOllamaConfig(cfg OllamaConfig) error {
	if cfg.URL == "" {
		cfg.URL = ollama.DefaultURL
	}
	if cfg.Model == "" {
		cfg.Model = ollama.DefaultModel
	}
	a.llm.URL = cfg.URL
	a.llm.Model = cfg.Model
	a.orch.SetLLM(a.llm)
	if a.store != nil {
		_ = a.store.SetSetting("ollama_url", cfg.URL)
		_ = a.store.SetSetting("ollama_model", cfg.Model)
	}
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

// SpawnDemo runs `claude --version` in the cwd and streams output to the frontend.
// Day-1 deliverable per §18.
func (a *App) SpawnDemo() (*types.Session, error) {
	cwd, _ := os.Getwd()
	ws := types.Workspace{ID: "demo", RepoPath: cwd}
	return a.mgr.SpawnRaw(ws, "claude", []string{"--version"})
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
	return a.mgr.SpawnPlan(ws, types.Issue{Number: issueNumber, WorkspaceID: workspaceID})
}

// ListSessions returns the live in-process sessions plus any running rows
// from the store that haven't been re-attached.
func (a *App) ListSessions() []types.Session {
	if a.mgr == nil {
		return nil
	}
	return a.mgr.Snapshot()
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

// SendInput writes user input to a PTY session.
func (a *App) SendInput(id, text string) error { return a.mgr.SendInput(id, text) }

// Notify shows an OS notification.
func (a *App) Notify(title, body string) error { return notify.Send(title, body) }

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
