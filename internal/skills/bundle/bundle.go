// Package bundle ships the four universal skills with the binary (PRISMCONDUCTOR_PLAN.md §15.7).
package bundle

import (
	"embed"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
)

//go:embed skills/*.md
var FS embed.FS

// Extract copies bundled skills to dst if they don't already exist.
// Returns the destination dir.
func Extract(dst string) (string, error) {
	if err := os.MkdirAll(dst, 0o755); err != nil {
		return "", err
	}
	entries, err := fs.ReadDir(FS, "skills")
	if err != nil {
		return "", err
	}
	for _, e := range entries {
		out := filepath.Join(dst, e.Name())
		if _, err := os.Stat(out); err == nil {
			continue // user may have edited; never overwrite
		}
		b, err := FS.ReadFile("skills/" + e.Name())
		if err != nil {
			return "", err
		}
		if err := os.WriteFile(out, b, 0o644); err != nil {
			return "", err
		}
	}
	return dst, nil
}

// InstallAsCommands writes (or refreshes) each bundled skill into Claude Code's
// user-scoped commands directory (~/.claude/commands/) so the spawned worker
// can resolve slash names like "/conductor-plan". Unlike Extract, this DOES
// overwrite — Claude Code resolves slash commands by filename, and the user
// editing one of these would more sensibly happen via the on-disk override at
// <cfgDir>/skills/, which Extract preserves.
func InstallAsCommands() error {
	home, err := os.UserHomeDir()
	if err != nil {
		return err
	}
	dst := filepath.Join(home, ".claude", "commands")
	if err := os.MkdirAll(dst, 0o755); err != nil {
		return err
	}
	entries, err := fs.ReadDir(FS, "skills")
	if err != nil {
		return err
	}
	for _, e := range entries {
		b, err := FS.ReadFile("skills/" + e.Name())
		if err != nil {
			return err
		}
		out := filepath.Join(dst, e.Name())
		if err := os.WriteFile(out, b, 0o644); err != nil {
			return err
		}
	}
	return nil
}

// CommandsDir returns the path Claude Code reads for user-scoped slash commands.
func CommandsDir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	dir := filepath.Join(home, ".claude", "commands")
	if _, err := os.Stat(dir); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return dir, nil
		}
		return "", err
	}
	return dir, nil
}
