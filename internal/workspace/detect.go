package workspace

import (
	"os"
	"path/filepath"

	"prismconductor/internal/types"
)

// DetectSkillProfile auto-fills SkillProfile by scanning <repo>/.claude/skills/ (§6.1.1).
// Phase 4.5 work — deeper detection lives there. Stub: returns Bundled by default.
func DetectSkillProfile(repoPath string) types.SkillProfile {
	skillsDir := filepath.Join(repoPath, ".claude", "skills")
	entries, err := os.ReadDir(skillsDir)
	if err != nil {
		return types.SkillProfile{Mode: types.SkillModeBundled, UseConductorPlan: true, UseConductorExecute: true, UseConductorClose: true}
	}
	hasStartIssue := false
	hasCheckClose := false
	for _, e := range entries {
		switch e.Name() {
		case "start-issue.md":
			hasStartIssue = true
		case "check-and-close.md":
			hasCheckClose = true
		}
	}
	switch {
	case hasStartIssue && hasCheckClose:
		return types.SkillProfile{
			Mode:                 types.SkillModeNative,
			NativePlanCommand:    "/start-issue",
			NativeExecuteCommand: "/start-issue",
			NativeCloseCommand:   "/check-and-close",
		}
	case hasStartIssue:
		return types.SkillProfile{
			Mode:                 types.SkillModeHybrid,
			NativePlanCommand:    "/start-issue",
			NativeExecuteCommand: "/start-issue",
			UseConductorClose:    true,
		}
	default:
		return types.SkillProfile{Mode: types.SkillModeBundled, UseConductorPlan: true, UseConductorExecute: true, UseConductorClose: true}
	}
}
