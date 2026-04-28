package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"runtime"

	wruntime "github.com/wailsapp/wails/v2/pkg/runtime"

	"prismconductor/internal/eventbus"
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
}

func NewApp() *App { return &App{} }

func (a *App) startup(ctx context.Context) {
	a.ctx = ctx

	cfgDir, _ := configDir()
	bundleDir := filepath.Join(cfgDir, "skills")
	if _, err := bundle.Extract(bundleDir); err != nil {
		fmt.Fprintf(os.Stderr, "skill bundle extract: %v\n", err)
	}

	a.bus = eventbus.New()
	a.pool = workerpool.New(2)
	a.llm = ollama.New("", "")
	a.orch = orchestrator.New(a.bus, a.llm)

	if s, err := store.Open(cfgDir); err != nil {
		fmt.Fprintf(os.Stderr, "store open: %v\n", err)
	} else {
		a.store = s
	}
	if r, err := workspace.New(cfgDir); err != nil {
		fmt.Fprintf(os.Stderr, "workspace registry: %v\n", err)
	} else {
		a.wsReg = r
	}

	a.mgr = session.NewManager(a.bus, a.emitLine)
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

// SpawnDemo runs `claude --version` in the cwd and streams output to the frontend.
// Day-1 deliverable per §18.
func (a *App) SpawnDemo() (*types.Session, error) {
	cwd, _ := os.Getwd()
	ws := types.Workspace{ID: "demo", RepoPath: cwd}
	return a.mgr.SpawnRaw(ws, "claude", []string{"--version"})
}

// KillSession terminates a running session.
func (a *App) KillSession(id string) error { return a.mgr.Kill(id) }

// SendInput writes user input to a PTY session.
func (a *App) SendInput(id, text string) error { return a.mgr.SendInput(id, text) }

// Notify shows an OS notification.
func (a *App) Notify(title, body string) error { return notify.Send(title, body) }

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
