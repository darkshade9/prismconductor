package harness

import (
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// assembleSystemPromptWithMarkdown builds the system prompt using an
// explicitly provided skill markdown instead of reading from the bundle FS.
// Repo enrichment (CLAUDE.md, .claude/rules/) is appended as before.
func assembleSystemPromptWithMarkdown(skillMarkdown string, repoPath string) (string, error) {
	parts := []string{skillMarkdown}
	if repoPath != "" {
		if b, err := os.ReadFile(filepath.Join(repoPath, "CLAUDE.md")); err == nil {
			parts = append(parts, "# Repo CLAUDE.md\n\n"+string(b))
		}
		rulesDir := filepath.Join(repoPath, ".claude", "rules")
		if entries, err := os.ReadDir(rulesDir); err == nil {
			names := make([]string, 0, len(entries))
			for _, e := range entries {
				if !e.IsDir() && strings.HasSuffix(e.Name(), ".md") {
					names = append(names, e.Name())
				}
			}
			sort.Strings(names)
			for _, name := range names {
				if b, err := os.ReadFile(filepath.Join(rulesDir, name)); err == nil {
					parts = append(parts, "# Repo rule: "+name+"\n\n"+string(b))
				}
			}
		}
	}
	return strings.Join(parts, "\n\n---\n\n"), nil
}

// assembleSystemPrompt builds the system prompt for a harness run.
// In Bundled mode (the only mode v1 supports — see §10.5), it concatenates:
//
//  1. The bundled skill markdown (`conductor-plan.md` for ModePlan,
//     `conductor-execute.md` for ModeExecute) read from the embedded
//     skills/ FS.
//  2. The repo's `CLAUDE.md` (silently skipped if absent — §15.8).
//  3. Every `.md` under the repo's `.claude/rules/` (silently skipped if
//     absent).
//
// The user message is built separately by the session manager
// (`planPrompt` / `executePrompt`); this function only assembles the system
// half so the session manager doesn't need to know which skill markdown to
// read.
func assembleSystemPrompt(skills fs.FS, mode string, repoPath string) (string, error) {
	skillFile := "skills/conductor-plan.md"
	if mode == "execute" {
		skillFile = "skills/conductor-execute.md"
	}
	parts := []string{}
	if b, err := fs.ReadFile(skills, skillFile); err == nil {
		parts = append(parts, string(b))
	} else {
		return "", err
	}
	if repoPath != "" {
		if b, err := os.ReadFile(filepath.Join(repoPath, "CLAUDE.md")); err == nil {
			parts = append(parts, "# Repo CLAUDE.md\n\n"+string(b))
		}
		rulesDir := filepath.Join(repoPath, ".claude", "rules")
		if entries, err := os.ReadDir(rulesDir); err == nil {
			names := make([]string, 0, len(entries))
			for _, e := range entries {
				if !e.IsDir() && strings.HasSuffix(e.Name(), ".md") {
					names = append(names, e.Name())
				}
			}
			sort.Strings(names)
			for _, name := range names {
				if b, err := os.ReadFile(filepath.Join(rulesDir, name)); err == nil {
					parts = append(parts, "# Repo rule: "+name+"\n\n"+string(b))
				}
			}
		}
	}
	return strings.Join(parts, "\n\n---\n\n"), nil
}
