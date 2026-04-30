package planio

import (
	"os"
	"path/filepath"
	"testing"
)

func writeTemp(t *testing.T, body string) string {
	t.Helper()
	dir := t.TempDir()
	p := filepath.Join(dir, "plan.json")
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatalf("write tmp plan: %v", err)
	}
	return p
}

// TestReadPlanCanonicalShape pins the long-standing happy path: a Claude-shaped
// plan with `issue_number` / `revision` reads cleanly without `workspace_id`
// being present (it is attached post-read by the conductor — issue #67).
func TestReadPlanCanonicalShape(t *testing.T) {
	p := writeTemp(t, `{
		"issue_number": 42,
		"revision": 1,
		"plan_markdown": "do the thing",
		"files_to_modify": [],
		"dependencies_detected": [],
		"questions": [],
		"estimated_complexity": "S",
		"ready_to_execute": false
	}`)
	plan, err := ReadPlan(p)
	if err != nil {
		t.Fatalf("ReadPlan canonical: %v", err)
	}
	if plan.IssueNumber != 42 || plan.Revision != 1 {
		t.Errorf("got issue=%d rev=%d, want 42/1", plan.IssueNumber, plan.Revision)
	}
}

// TestReadPlanAcceptsAbbreviatedAliases pins the leniency added in #67:
// non-Claude planners (gpt-5-mini observed in the wild) emit `issue` / `rev`
// instead of the canonical names. The validator normalizes rather than
// rejecting silently.
func TestReadPlanAcceptsAbbreviatedAliases(t *testing.T) {
	p := writeTemp(t, `{
		"issue": 53,
		"rev": 1,
		"plan_markdown": "x",
		"files_to_modify": [],
		"dependencies_detected": [],
		"questions": [],
		"estimated_complexity": "S",
		"ready_to_execute": false
	}`)
	plan, err := ReadPlan(p)
	if err != nil {
		t.Fatalf("ReadPlan with aliases: %v", err)
	}
	if plan.IssueNumber != 53 {
		t.Errorf("issue alias not applied, got %d", plan.IssueNumber)
	}
	if plan.Revision != 1 {
		t.Errorf("rev alias not applied, got %d", plan.Revision)
	}
}

// TestReadPlanCanonicalWinsOverAlias pins precedence: when both forms are
// present the canonical name takes priority. (Defensive — should never happen
// in practice, but locks in the rule.)
func TestReadPlanCanonicalWinsOverAlias(t *testing.T) {
	p := writeTemp(t, `{
		"issue_number": 100, "issue": 200,
		"revision": 5, "rev": 9,
		"plan_markdown": "x",
		"files_to_modify": [], "questions": []
	}`)
	plan, err := ReadPlan(p)
	if err != nil {
		t.Fatalf("ReadPlan precedence: %v", err)
	}
	if plan.IssueNumber != 100 || plan.Revision != 5 {
		t.Errorf("got issue=%d rev=%d, want 100/5 (canonical wins)", plan.IssueNumber, plan.Revision)
	}
}

// TestReadPlanRejectsTrulyMissingFields pins that the leniency does not turn
// the validator into a doormat: a plan with neither canonical nor alias still
// errors so the conductor can flip the card to BLOCKED with a real message.
func TestReadPlanRejectsTrulyMissingFields(t *testing.T) {
	p := writeTemp(t, `{
		"plan_markdown": "x",
		"files_to_modify": [],
		"questions": []
	}`)
	if _, err := ReadPlan(p); err == nil {
		t.Fatal("expected error for plan with no issue id, got nil")
	}
}

// TestReadPlanRejectsInvalidJSON locks the JSON-decode error path: a malformed
// body returns a wrapped error so the conductor's BLOCKED message includes the
// actual decode failure.
func TestReadPlanRejectsInvalidJSON(t *testing.T) {
	p := writeTemp(t, `{not json}`)
	if _, err := ReadPlan(p); err == nil {
		t.Fatal("expected JSON decode error, got nil")
	}
}
