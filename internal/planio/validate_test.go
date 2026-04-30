package planio

import (
	"strings"
	"testing"
)

func mustValid(t *testing.T, body string) {
	t.Helper()
	if err := ValidatePlanJSON([]byte(body)); err != nil {
		t.Fatalf("expected valid plan, got error: %v", err)
	}
}

func mustReject(t *testing.T, body, expectSubstr string) {
	t.Helper()
	err := ValidatePlanJSON([]byte(body))
	if err == nil {
		t.Fatalf("expected rejection containing %q, got nil", expectSubstr)
	}
	if !strings.Contains(err.Error(), expectSubstr) {
		t.Fatalf("expected error to contain %q, got: %v", expectSubstr, err)
	}
}

const minimalValidPlan = `{
	"issue_number": 7,
	"revision": 1,
	"plan_markdown": "do the thing",
	"files_to_modify": [],
	"dependencies_detected": [],
	"questions": [],
	"estimated_complexity": "S",
	"ready_to_execute": false
}`

// Plan with every field populated correctly should validate cleanly.
func TestValidatePlanJSON_HappyPath(t *testing.T) {
	mustValid(t, `{
		"issue_number": 42,
		"revision": 2,
		"goal_summary": "...",
		"executive_summary": "...",
		"plan_markdown": "## Plan\n...",
		"files_to_modify": [
			{ "path": "frontend/src/Card.tsx", "intent": "modify" },
			{ "path": "internal/llm/foo.go", "intent": "add" }
		],
		"dependencies_detected": [13, 27],
		"suggested_labels": ["enhancement", "frontend"],
		"questions": [
			{
				"id": "q1",
				"type": "single_choice",
				"prompt": "Which approach?",
				"options": ["A", "B"],
				"default": "A",
				"required": true
			},
			{
				"id": "q2",
				"type": "yes_no",
				"prompt": "Bundle?",
				"default": "no",
				"required": false
			},
			{
				"id": "q3",
				"type": "free_text",
				"prompt": "Notes?",
				"default": "no notes",
				"required": false
			}
		],
		"estimated_complexity": "M",
		"ready_to_execute": false
	}`)
}

func TestValidatePlanJSON_Minimal(t *testing.T) {
	mustValid(t, minimalValidPlan)
}

// `issue` instead of `issue_number` is the most common gpt-5-mini failure
// mode. The validator must call out the rule explicitly so the model can fix
// it on the next turn.
func TestValidatePlanJSON_RejectsIssueAlias(t *testing.T) {
	mustReject(t, `{
		"issue": 7, "revision": 1,
		"plan_markdown": "x", "files_to_modify": [], "dependencies_detected": [],
		"questions": [], "estimated_complexity": "S", "ready_to_execute": false
	}`, "issue_number")
}

func TestValidatePlanJSON_RejectsRevAlias(t *testing.T) {
	mustReject(t, `{
		"issue_number": 7, "rev": 1,
		"plan_markdown": "x", "files_to_modify": [], "dependencies_detected": [],
		"questions": [], "estimated_complexity": "S", "ready_to_execute": false
	}`, "revision")
}

// Some models emit files_to_modify as a {add, modify, delete} object instead
// of an array. Specifically reject so the error message points at the shape.
func TestValidatePlanJSON_RejectsFilesToModifyAsObject(t *testing.T) {
	mustReject(t, `{
		"issue_number": 7, "revision": 1, "plan_markdown": "x",
		"files_to_modify": {"add": ["foo"], "modify": [], "delete": []},
		"dependencies_detected": [], "questions": [],
		"estimated_complexity": "S", "ready_to_execute": false
	}`, "{add, modify, delete}")
}

func TestValidatePlanJSON_RejectsFileIntentMissingPath(t *testing.T) {
	mustReject(t, `{
		"issue_number": 7, "revision": 1, "plan_markdown": "x",
		"files_to_modify": [{"intent": "modify"}],
		"dependencies_detected": [], "questions": [],
		"estimated_complexity": "S", "ready_to_execute": false
	}`, "files_to_modify[0].path")
}

func TestValidatePlanJSON_RejectsLabelWithAnnotation(t *testing.T) {
	mustReject(t, `{
		"issue_number": 7, "revision": 1, "plan_markdown": "x",
		"files_to_modify": [], "dependencies_detected": [],
		"suggested_labels": ["enhancement (create)"],
		"questions": [], "estimated_complexity": "S", "ready_to_execute": false
	}`, "label-with-annotation")
}

func TestValidatePlanJSON_RejectsBadQuestionType(t *testing.T) {
	mustReject(t, `{
		"issue_number": 7, "revision": 1, "plan_markdown": "x",
		"files_to_modify": [], "dependencies_detected": [],
		"questions": [{"id": "q1", "type": "checkbox", "prompt": "?", "default": "x"}],
		"estimated_complexity": "S", "ready_to_execute": false
	}`, "single_choice / multi_choice / free_text / yes_no")
}

func TestValidatePlanJSON_RejectsQuestionMissingDefault(t *testing.T) {
	mustReject(t, `{
		"issue_number": 7, "revision": 1, "plan_markdown": "x",
		"files_to_modify": [], "dependencies_detected": [],
		"questions": [{"id": "q1", "type": "yes_no", "prompt": "?"}],
		"estimated_complexity": "S", "ready_to_execute": false
	}`, "default")
}

func TestValidatePlanJSON_RejectsSingleChoiceMissingOptions(t *testing.T) {
	mustReject(t, `{
		"issue_number": 7, "revision": 1, "plan_markdown": "x",
		"files_to_modify": [], "dependencies_detected": [],
		"questions": [{"id": "q1", "type": "single_choice", "prompt": "?", "default": "A"}],
		"estimated_complexity": "S", "ready_to_execute": false
	}`, "options")
}

func TestValidatePlanJSON_RejectsZeroIssueNumber(t *testing.T) {
	mustReject(t, `{
		"issue_number": 0, "revision": 1, "plan_markdown": "x",
		"files_to_modify": [], "dependencies_detected": [],
		"questions": [], "estimated_complexity": "S", "ready_to_execute": false
	}`, "issue_number")
}

func TestValidatePlanJSON_RejectsMalformedJSON(t *testing.T) {
	mustReject(t, `{not json`, "valid JSON")
}

func TestValidatePlanJSON_RejectsMissingReadyToExecute(t *testing.T) {
	mustReject(t, `{
		"issue_number": 7, "revision": 1, "plan_markdown": "x",
		"files_to_modify": [], "dependencies_detected": [],
		"questions": [], "estimated_complexity": "S"
	}`, "ready_to_execute")
}
