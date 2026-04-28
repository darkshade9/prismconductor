package orchestrator

// SystemPromptRankDeps is the system prompt for the rank+deps call (§11.2).
const SystemPromptRankDeps = `You are a backlog organizer. Given a goal and a list of GitHub issues, you produce
a JSON object describing dependency relationships and a priority ordering.

Rules:
- A "primitive" is an issue that other issues depend on (foundational work).
- A "dependent" is an issue that needs primitives done first.
- Use issue body text mentioning "depends on", "requires", "blocked by", or
  numeric issue references (#NNNN) as hard signals.
- Use semantic understanding to infer dependencies when not explicit.
- Output valid JSON only. No prose. No markdown fences.`

// UserPromptRankDepsTemplate is the per-event user prompt; rendered with text/template.
const UserPromptRankDepsTemplate = `GOAL:
Title: {{.Goal.Title}}
Intent: {{.Goal.Intent}}
Acceptance: {{.Goal.AcceptanceRule}}

ISSUES (id, title, body excerpt):
{{range .Issues}}
#{{.Number}} {{.Title}}
{{.BodyExcerpt}}
---
{{end}}

Return JSON:
{
  "ordering": [<issue_number>, ...],
  "dependencies": [
    {"issue": <num>, "depends_on": [<num>, ...], "rationale": "..."}
  ],
  "primitives": [<issue_number>, ...],
  "rationale": "one paragraph summary"
}`

type RankDepsResult struct {
	Ordering     []int            `json:"ordering"`
	Dependencies []DependencyEdge `json:"dependencies"`
	Primitives   []int            `json:"primitives"`
	Rationale    string           `json:"rationale"`
}

type DependencyEdge struct {
	Issue     int    `json:"issue"`
	DependsOn []int  `json:"depends_on"`
	Rationale string `json:"rationale"`
}
