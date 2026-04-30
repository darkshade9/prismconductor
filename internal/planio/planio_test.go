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

// TestReadPlanCanonicalShape pins the happy path: a §9.1-shaped plan reads
// cleanly without `workspace_id` (the conductor attaches it post-read).
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

// TestReadPlanRejectsAbbreviatedAliases pins the strict-contract direction
// from #71: the previous lenient acceptance of `issue` / `rev` is gone. Every
// model — Claude, gpt-5-mini, Gemini, qwen3 — must emit the canonical names
// or the plan is rejected with a specific actionable error.
func TestReadPlanRejectsAbbreviatedAliases(t *testing.T) {
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
	_, err := ReadPlan(p)
	if err == nil {
		t.Fatal("expected validation error rejecting `issue`/`rev` alias, got nil")
	}
	// The message should name the offending field so a model reading it in
	// a tool_result knows what to fix.
	msg := err.Error()
	if !contains(msg, "issue_number") || !contains(msg, "issue") {
		t.Errorf("error message %q should call out `issue` and recommend `issue_number`", msg)
	}
}

func contains(haystack, needle string) bool {
	for i := 0; i+len(needle) <= len(haystack); i++ {
		if haystack[i:i+len(needle)] == needle {
			return true
		}
	}
	return false
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
