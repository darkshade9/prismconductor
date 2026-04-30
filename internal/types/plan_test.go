package types

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestPlanGoalAndExecutiveSummaryOmitempty(t *testing.T) {
	p := Plan{
		IssueNumber:  55,
		WorkspaceID:  "prismconductor",
		Revision:     1,
		PlanMarkdown: "## hi",
	}
	out, err := json.Marshal(p)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	s := string(out)
	if strings.Contains(s, "goal_summary") {
		t.Errorf("expected omitempty to drop goal_summary when empty; got %s", s)
	}
	if strings.Contains(s, "executive_summary") {
		t.Errorf("expected omitempty to drop executive_summary when empty; got %s", s)
	}
}

func TestPlanGoalAndExecutiveSummaryRoundTrip(t *testing.T) {
	p := Plan{
		IssueNumber:      55,
		WorkspaceID:      "prismconductor",
		Revision:         2,
		GoalSummary:      "Restate the issue intent.",
		ExecutiveSummary: "Outcome the user gets when this ships.",
		PlanMarkdown:     "## steps\n1. do thing",
	}
	out, err := json.Marshal(p)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var got Plan
	if err := json.Unmarshal(out, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got.GoalSummary != p.GoalSummary {
		t.Errorf("goal_summary round-trip: got %q want %q", got.GoalSummary, p.GoalSummary)
	}
	if got.ExecutiveSummary != p.ExecutiveSummary {
		t.Errorf("executive_summary round-trip: got %q want %q", got.ExecutiveSummary, p.ExecutiveSummary)
	}
}

// Old plans on disk (written before #55) lack the new fields entirely. The
// schema-level "omitempty + non-pointer string" contract is what lets them
// keep deserializing cleanly — this test pins that behavior.
func TestPlanLegacyJSONDeserializesCleanly(t *testing.T) {
	legacy := []byte(`{
		"issue_number": 1,
		"workspace_id": "demo",
		"revision": 1,
		"plan_markdown": "## old plan",
		"files_to_modify": [],
		"dependencies_detected": [],
		"questions": [],
		"estimated_complexity": "small",
		"ready_to_execute": true,
		"generated_at": "2025-01-01T00:00:00Z"
	}`)
	var p Plan
	if err := json.Unmarshal(legacy, &p); err != nil {
		t.Fatalf("legacy unmarshal: %v", err)
	}
	if p.GoalSummary != "" {
		t.Errorf("expected empty GoalSummary on legacy plan; got %q", p.GoalSummary)
	}
	if p.ExecutiveSummary != "" {
		t.Errorf("expected empty ExecutiveSummary on legacy plan; got %q", p.ExecutiveSummary)
	}
	if p.PlanMarkdown != "## old plan" {
		t.Errorf("plan_markdown not preserved: %q", p.PlanMarkdown)
	}
}
