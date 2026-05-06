package tests

import (
	"strings"
	"testing"

	"prismconductor/internal/skills/bundle"
	"prismconductor/internal/skills/validator"
)

// TestConductorPlanSkillDocContainsBLOCKEDRule verifies that the bundled
// conductor-plan.md skill doc includes the BLOCKED-on-fetch-failure rule
// (issue #232). The skill doc is the authoritative instruction to the planner
// model; this test is a canary that fires if someone edits it away.
func TestConductorPlanSkillDocContainsBLOCKEDRule(t *testing.T) {
	content, err := bundle.FS.ReadFile("skills/conductor-plan.md")
	if err != nil {
		t.Fatalf("cannot read bundled conductor-plan.md: %v", err)
	}
	text := string(content)

	required := []string{
		"BLOCKED on fetch failure",
		"BLOCKED: cannot fetch issue",
		"ready_to_execute: true",
	}
	for _, phrase := range required {
		if !strings.Contains(text, phrase) {
			t.Errorf("conductor-plan.md missing required phrase %q — the BLOCKED-on-fetch-failure rule may have been removed", phrase)
		}
	}
}

// TestBannedPhrasesListIsNonEmpty ensures the validator's banned-phrase list
// has at least the core set of fetch-failure indicators.
func TestBannedPhrasesListIsNonEmpty(t *testing.T) {
	if len(validator.BannedPhrases) == 0 {
		t.Fatal("validator.BannedPhrases is empty — no fetch-failure detection possible")
	}
	mustContain := []string{
		"i couldn't fetch",
		"failed to retrieve the issue",
		"could not access the issue",
	}
	for _, phrase := range mustContain {
		found := false
		for _, bp := range validator.BannedPhrases {
			if bp == phrase {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("validator.BannedPhrases is missing core phrase %q", phrase)
		}
	}
}

// TestFetchFailurePhraseDetectedInPlanMarkdown is an integration-style smoke
// test: a plan whose plan_markdown contains a fetch-failure admission triggers
// the content validator when ready_to_execute is true.
func TestFetchFailurePhraseDetectedInPlanMarkdown(t *testing.T) {
	planJSON := `{
		"issue_number": 99,
		"revision": 1,
		"plan_markdown": "I couldn't fetch the issue body from GitHub due to auth error.",
		"goal_summary": "Unknown.",
		"executive_summary": "Unknown.",
		"files_to_modify": [],
		"dependencies_detected": [],
		"questions": [],
		"estimated_complexity": "S",
		"ready_to_execute": true
	}`
	err := validator.ValidatePlanContent([]byte(planJSON), 0, "")
	if err == nil {
		t.Fatal("expected content validator to reject plan with banned phrase + ready_to_execute:true")
	}
}

// TestFetchFailurePhraseAllowedWhenNotReady verifies that a plan with a
// fetch-failure phrase but ready_to_execute: false passes the content
// validator (the planner correctly declined to mark it ready).
func TestFetchFailurePhraseAllowedWhenNotReady(t *testing.T) {
	planJSON := `{
		"issue_number": 99,
		"revision": 1,
		"plan_markdown": "I couldn't fetch the issue — please paste the body.",
		"goal_summary": "",
		"executive_summary": "",
		"files_to_modify": [],
		"dependencies_detected": [],
		"questions": [],
		"estimated_complexity": "S",
		"ready_to_execute": false
	}`
	if err := validator.ValidatePlanContent([]byte(planJSON), 0, ""); err != nil {
		t.Fatalf("expected plan with fetch-failure phrase but ready:false to pass, got: %v", err)
	}
}
