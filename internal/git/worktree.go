// Package git wraps the small set of `git worktree` operations the session
// manager needs to spawn and reap per-execute worktrees (issue #22).
package git

import (
	"fmt"
	"os/exec"
	"strings"
)

// Add fetches origin and creates a worktree at dir on a fresh branch off
// origin/<base>. Uses `-B` so a leftover branch from a prior failed run is
// reset rather than reported as a conflict.
func Add(repoPath, branch, dir, base string) error {
	if _, err := run(repoPath, "git", "fetch", "origin"); err != nil {
		return fmt.Errorf("fetch origin: %w", err)
	}
	out, err := run(repoPath, "git", "worktree", "add", "-B", branch, dir, "origin/"+base)
	if err != nil {
		return fmt.Errorf("worktree add %s -> %s: %w (output: %s)", branch, dir, err, out)
	}
	return nil
}

// Remove tears down a worktree directory and deregisters it. Force-flag is
// always set: the conductor never asks for graceful removal because the worker
// process owns the tree and the conductor's only opportunity to clean is post-
// session.
func Remove(repoPath, dir string) error {
	out, err := run(repoPath, "git", "worktree", "remove", "--force", dir)
	if err != nil {
		return fmt.Errorf("worktree remove %s: %w (output: %s)", dir, err, out)
	}
	return nil
}

// Prune deregisters worktrees whose directories no longer exist on disk.
func Prune(repoPath string) error {
	_, err := run(repoPath, "git", "worktree", "prune")
	return err
}

// Entry is one line of `git worktree list --porcelain` output.
type Entry struct {
	Path   string
	Branch string
	HEAD   string
}

// List returns the registered worktrees for repoPath, including the primary
// checkout. Caller filters by path prefix.
func List(repoPath string) ([]Entry, error) {
	out, err := run(repoPath, "git", "worktree", "list", "--porcelain")
	if err != nil {
		return nil, err
	}
	return parseWorktreeList(out), nil
}

// HasSubmodules reports whether the worktree has a .gitmodules file at its
// root. Called after Add (q4=D) so InitSubmodules only fires on repos that
// actually use submodules.
func HasSubmodules(worktreeDir string) bool {
	_, err := run(worktreeDir, "test", "-f", ".gitmodules")
	return err == nil
}

// InitSubmodules runs `git submodule update --init --recursive` from inside
// the worktree. Slow for large submodules; this is the documented cost of the
// q4=D "complete checkout" choice.
func InitSubmodules(worktreeDir string) error {
	out, err := run(worktreeDir, "git", "submodule", "update", "--init", "--recursive")
	if err != nil {
		return fmt.Errorf("submodule init: %w (output: %s)", err, out)
	}
	return nil
}

func parseWorktreeList(out string) []Entry {
	var entries []Entry
	var cur Entry
	flush := func() {
		if cur.Path != "" {
			entries = append(entries, cur)
		}
		cur = Entry{}
	}
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			flush()
			continue
		}
		switch {
		case strings.HasPrefix(line, "worktree "):
			cur.Path = strings.TrimPrefix(line, "worktree ")
		case strings.HasPrefix(line, "HEAD "):
			cur.HEAD = strings.TrimPrefix(line, "HEAD ")
		case strings.HasPrefix(line, "branch "):
			cur.Branch = strings.TrimPrefix(line, "branch ")
		}
	}
	flush()
	return entries
}

func run(dir, name string, args ...string) (string, error) {
	cmd := exec.Command(name, args...)
	if dir != "" {
		cmd.Dir = dir
	}
	out, err := cmd.CombinedOutput()
	return string(out), err
}
