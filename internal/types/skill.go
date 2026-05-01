package types

// ConductorStage identifies which step in the conductor loop a skill serves.
type ConductorStage string

const (
	StagePlan     ConductorStage = "plan"
	StageExecute  ConductorStage = "execute"
	StageContinue ConductorStage = "continue"
	StageClose    ConductorStage = "close"
)

// AllStages returns the four conductor stages in canonical order.
func AllStages() []ConductorStage {
	return []ConductorStage{StagePlan, StageExecute, StageContinue, StageClose}
}

// SkillRef points to one skill markdown file. Source is "bundled" or "repo".
// Bundled skills use the sentinel Path "bundled:<name>" (e.g. "bundled:conductor-plan").
// Repo skills use the absolute filesystem path of the markdown file.
type SkillRef struct {
	Path        string `json:"path"`
	Source      string `json:"source"`
	Name        string `json:"name"`
	DisplayName string `json:"display_name"`
	Description string `json:"description,omitempty"`
}
