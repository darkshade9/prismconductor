// Package workspace owns the workspace registry and onboarding checks (PRISMCONDUCTOR_PLAN.md §3, §6.1).
package workspace

import (
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"prismconductor/internal/types"
)

type Registry struct {
	Path  string // workspaces.json path
	items []types.Workspace
}

func New(configDir string) (*Registry, error) {
	if err := os.MkdirAll(configDir, 0o755); err != nil {
		return nil, err
	}
	r := &Registry{Path: filepath.Join(configDir, "workspaces.json")}
	if err := r.load(); err != nil {
		return nil, err
	}
	return r, nil
}

func (r *Registry) load() error {
	b, err := os.ReadFile(r.Path)
	if errors.Is(err, os.ErrNotExist) {
		r.items = nil
		return nil
	}
	if err != nil {
		return err
	}
	return json.Unmarshal(b, &r.items)
}

func (r *Registry) save() error {
	b, _ := json.MarshalIndent(r.items, "", "  ")
	return os.WriteFile(r.Path, b, 0o644)
}

func (r *Registry) List() []types.Workspace { return append([]types.Workspace(nil), r.items...) }

func (r *Registry) Add(ws types.Workspace) error {
	r.items = append(r.items, ws)
	return r.save()
}

// Onboard runs the four §3 checks against a candidate path.
type OnboardCheck struct {
	Name string
	Pass bool
	Info string
}

func Onboard(repoPath string) []OnboardCheck {
	checks := []OnboardCheck{}

	// 1. git repo
	out, err := runIn(repoPath, "git", "rev-parse", "--git-dir")
	checks = append(checks, OnboardCheck{Name: "git repo", Pass: err == nil, Info: strings.TrimSpace(out)})

	// 2. github remote
	out, err = runIn(repoPath, "git", "remote", "get-url", "origin")
	checks = append(checks, OnboardCheck{Name: "github remote", Pass: err == nil, Info: strings.TrimSpace(out)})

	// 3. gh auth
	out, err = runIn(repoPath, "gh", "auth", "status")
	checks = append(checks, OnboardCheck{Name: "gh auth", Pass: err == nil, Info: strings.TrimSpace(out)})

	// 4. claude on PATH
	pth, err := exec.LookPath("claude")
	checks = append(checks, OnboardCheck{Name: "claude CLI", Pass: err == nil, Info: pth})

	return checks
}

func runIn(dir, name string, args ...string) (string, error) {
	cmd := exec.Command(name, args...)
	if dir != "" {
		cmd.Dir = dir
	}
	out, err := cmd.CombinedOutput()
	return string(out), err
}
