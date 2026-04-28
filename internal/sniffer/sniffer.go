// Package sniffer is the Phase 7 deterministic convention detector (PRISMCONDUCTOR_PLAN.md §15.10).
package sniffer

import (
	"os"
	"path/filepath"

	"prismconductor/internal/types"
)

// Sniff produces ConventionHints for a repo. Stub: detects package manager only.
func Sniff(repoPath string) types.ConventionHints {
	hints := types.ConventionHints{}
	switch {
	case fileExists(filepath.Join(repoPath, "go.mod")):
		hints.PackageManager = "go"
		hints.BuildCommand = "go build ./..."
		hints.TestCommand = "go test ./..."
	case fileExists(filepath.Join(repoPath, "pnpm-lock.yaml")):
		hints.PackageManager = "pnpm"
	case fileExists(filepath.Join(repoPath, "yarn.lock")):
		hints.PackageManager = "yarn"
	case fileExists(filepath.Join(repoPath, "package-lock.json")):
		hints.PackageManager = "npm"
	case fileExists(filepath.Join(repoPath, "pyproject.toml")) || fileExists(filepath.Join(repoPath, "requirements.txt")):
		hints.PackageManager = "pip"
		if fileExists(filepath.Join(repoPath, ".venv", "bin", "python")) {
			hints.PyEnvPath = ".venv/bin/python"
		}
	}
	return hints
}

func fileExists(p string) bool {
	_, err := os.Stat(p)
	return err == nil
}
