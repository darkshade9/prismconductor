// Package skills provides skill discovery and loading helpers (issue #92).
package skills

import (
	"io/fs"
	"path/filepath"
	"strings"

	"prismconductor/internal/types"
)

// repoGlobs are the directory patterns searched for repo-supplied skill files,
// in priority order. PrismConductor's own directory is first so repo authors
// can explicitly ship overrides there; common tool directories follow.
// Each root is listed twice: the flat *.md form and the one-level subdirectory
// form (skill-name/SKILL.md). filepath.Glob is non-recursive, so both casings
// of the sentinel filename are listed explicitly for Linux compatibility.
var repoGlobs = []string{
	".prismconductor/skills/*.md",
	".prismconductor/skills/*/SKILL.md",
	".prismconductor/skills/*/skill.md",
	".claude/commands/*.md",
	".claude/commands/*/SKILL.md",
	".claude/commands/*/skill.md",
	".claude/skills/*.md",
	".claude/skills/*/SKILL.md",
	".claude/skills/*/skill.md",
	".cursor/rules/*.md",
	".cursor/rules/*/SKILL.md",
	".cursor/rules/*/skill.md",
	".cursor/commands/*.md",
	".cursor/commands/*/SKILL.md",
	".cursor/commands/*/skill.md",
	".codex/skills/*.md",
	".codex/skills/*/SKILL.md",
	".codex/skills/*/skill.md",
	".codex/commands/*.md",
	".codex/commands/*/SKILL.md",
	".codex/commands/*/skill.md",
}

// Discover returns all available skills for the given workspace. Bundled
// skills are always listed first. A repo file with the same basename as a
// bundled skill shadows the bundled entry (issue #92 q2: repo overrides
// bundled). Discovery errors inside the repo tree are silently skipped so a
// missing or unreadable directory never blocks a spawn.
func Discover(repoPath string, bundleFS fs.FS) ([]types.SkillRef, error) {
	refs, err := listBundled(bundleFS)
	if err != nil {
		return nil, err
	}

	// Build name→index so repo overrides can replace in-place.
	byName := make(map[string]int, len(refs))
	for i, r := range refs {
		byName[r.Name] = i
	}

	if repoPath != "" {
		for _, ref := range discoverRepo(repoPath) {
			if idx, ok := byName[ref.Name]; ok {
				refs[idx] = ref // repo file shadows the bundled entry
			} else {
				byName[ref.Name] = len(refs)
				refs = append(refs, ref)
			}
		}
	}

	return refs, nil
}

func listBundled(bundleFS fs.FS) ([]types.SkillRef, error) {
	entries, err := fs.ReadDir(bundleFS, "skills")
	if err != nil {
		return nil, err
	}
	var out []types.SkillRef
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".md") {
			continue
		}
		name := strings.TrimSuffix(e.Name(), ".md")
		out = append(out, types.SkillRef{
			Path:        "bundled:" + name,
			Source:      "bundled",
			Name:        name,
			DisplayName: name,
		})
	}
	return out, nil
}

func discoverRepo(repoPath string) []types.SkillRef {
	seen := make(map[string]bool)
	var out []types.SkillRef
	for _, pattern := range repoGlobs {
		matches, _ := filepath.Glob(filepath.Join(repoPath, pattern))
		for _, match := range matches {
			if seen[match] {
				continue
			}
			seen[match] = true
			base := filepath.Base(match)
			var name string
			if strings.EqualFold(base, "skill.md") {
				// subdirectory layout: skill-name/SKILL.md — use parent dir as name
				name = filepath.Base(filepath.Dir(match))
			} else {
				name = strings.TrimSuffix(base, ".md")
			}
			out = append(out, types.SkillRef{
				Path:        match,
				Source:      "repo",
				Name:        name,
				DisplayName: name + " (repo)",
			})
		}
	}
	return out
}
