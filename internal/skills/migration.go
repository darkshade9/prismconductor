package skills

import "prismconductor/internal/types"

// defaultBundledRef creates a SkillRef pointing to a bundled skill by name.
func defaultBundledRef(name string) types.SkillRef {
	return types.SkillRef{
		Path:        "bundled:" + name,
		Source:      "bundled",
		Name:        name,
		DisplayName: name,
	}
}

// MigrateSkillProfile synthesizes PerStage from legacy SkillProfile fields
// (Mode + UseConductor* booleans) if migration has not yet run. Returns true
// when the profile was modified so the caller knows to persist the workspace.
//
// Policy (issue #92 q4): hybrid and native modes are "ambiguous" — their
// native shell commands are not markdown files and cannot be expressed as a
// SkillRef. All four stages therefore default to the canonical bundled skills.
// The legacy Mode/NativeCommand fields remain on disk for one release.
func MigrateSkillProfile(sp *types.SkillProfile) bool {
	if sp.SkillsMigrated {
		return false
	}
	sp.PerStage = map[string]types.SkillRef{
		string(types.StagePlan):     defaultBundledRef("conductor-plan"),
		string(types.StageExecute):  defaultBundledRef("conductor-execute"),
		string(types.StageContinue): defaultBundledRef("conductor-continue"),
		string(types.StageClose):    defaultBundledRef("conductor-close"),
	}
	sp.SkillsMigrated = true
	return true
}
