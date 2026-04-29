// Package workspace owns the workspace registry and onboarding checks (PRISMCONDUCTOR_PLAN.md §3, §6.1).
package workspace

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"

	"prismconductor/internal/sniffer"
	"prismconductor/internal/types"
)

type Registry struct {
	Path string

	mu    sync.RWMutex
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

func (r *Registry) List() []types.Workspace {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return append([]types.Workspace(nil), r.items...)
}

func (r *Registry) Get(id string) (types.Workspace, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	for _, w := range r.items {
		if w.ID == id {
			return w, true
		}
	}
	return types.Workspace{}, false
}

func (r *Registry) Add(ws types.Workspace) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if ws.ID == "" {
		return errors.New("workspace ID required")
	}
	for _, existing := range r.items {
		if existing.ID == ws.ID {
			return fmt.Errorf("workspace %q already exists", ws.ID)
		}
	}
	r.items = append(r.items, ws)
	return r.save()
}

func (r *Registry) Update(ws types.Workspace) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	for i, existing := range r.items {
		if existing.ID == ws.ID {
			r.items[i] = ws
			return r.save()
		}
	}
	return fmt.Errorf("workspace %q not found", ws.ID)
}

func (r *Registry) Remove(id string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	for i, existing := range r.items {
		if existing.ID == id {
			r.items = append(r.items[:i], r.items[i+1:]...)
			return r.save()
		}
	}
	return fmt.Errorf("workspace %q not found", id)
}

// Onboard runs the four §3 checks against a candidate path.
type OnboardCheck struct {
	Name string `json:"name"`
	Pass bool   `json:"pass"`
	Info string `json:"info"`
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

// Inspect returns the data needed to pre-fill an Add Workspace form for a given path:
// onboarding checks, parsed owner/repo, default branch, detected skill profile, and conventions.
type Inspection struct {
	RepoPath      string             `json:"repo_path"`
	Checks        []OnboardCheck     `json:"checks"`
	GitHubOwner   string             `json:"github_owner"`
	GitHubRepo    string             `json:"github_repo"`
	DefaultBranch string             `json:"default_branch"`
	SuggestedID   string             `json:"suggested_id"`
	SkillProfile  types.SkillProfile `json:"skill_profile"`
	Conventions   types.ConventionHints `json:"conventions"`
}

func Inspect(repoPath string) Inspection {
	insp := Inspection{
		RepoPath:     repoPath,
		Checks:       Onboard(repoPath),
		SkillProfile: DetectSkillProfile(repoPath),
		Conventions:  sniffer.Sniff(repoPath),
	}
	if remote, err := runIn(repoPath, "git", "remote", "get-url", "origin"); err == nil {
		owner, repo := parseRemote(strings.TrimSpace(remote))
		insp.GitHubOwner = owner
		insp.GitHubRepo = repo
		insp.SuggestedID = repo
	}
	if branch, err := runIn(repoPath, "git", "rev-parse", "--abbrev-ref", "origin/HEAD"); err == nil {
		b := strings.TrimSpace(branch)
		if idx := strings.LastIndex(b, "/"); idx >= 0 {
			b = b[idx+1:]
		}
		insp.DefaultBranch = b
	}
	if insp.DefaultBranch == "" {
		if branch, err := runIn(repoPath, "git", "symbolic-ref", "--short", "HEAD"); err == nil {
			insp.DefaultBranch = strings.TrimSpace(branch)
		}
	}
	return insp
}

// parseRemote extracts owner/repo from an origin URL (https or ssh).
func parseRemote(remote string) (owner, repo string) {
	remote = strings.TrimSuffix(remote, ".git")
	// git@github.com:owner/repo
	if i := strings.Index(remote, ":"); strings.HasPrefix(remote, "git@") && i > 0 {
		path := remote[i+1:]
		parts := strings.SplitN(path, "/", 2)
		if len(parts) == 2 {
			return parts[0], parts[1]
		}
	}
	// https://github.com/owner/repo
	if idx := strings.Index(remote, "github.com/"); idx >= 0 {
		path := remote[idx+len("github.com/"):]
		parts := strings.SplitN(path, "/", 2)
		if len(parts) == 2 {
			return parts[0], parts[1]
		}
	}
	return "", ""
}

func runIn(dir, name string, args ...string) (string, error) {
	cmd := exec.Command(name, args...)
	if dir != "" {
		cmd.Dir = dir
	}
	out, err := cmd.CombinedOutput()
	return string(out), err
}
