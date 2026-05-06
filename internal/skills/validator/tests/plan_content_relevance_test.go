package tests

import (
	"testing"

	"prismconductor/internal/skills/validator"
)

func TestContainsBannedPhrase(t *testing.T) {
	cases := []struct {
		text  string
		found bool
	}{
		{"I couldn't fetch the issue from GitHub.", true},
		{"I could not fetch the issue body.", true},
		{"Failed to retrieve the issue.", true},
		{"Could not access the issue.", true},
		{"Unable to fetch the issue — auth error.", true},
		{"I was unable to read the issue.", true},
		{"I could not read the issue body.", true},
		{"I couldn't read the issue.", true},
		{"Failed to fetch the issue.", true},
		{"Cannot fetch the issue.", true},
		{"Unable to retrieve the issue.", true},
		{"Couldn't access the issue.", true},
		// Case-insensitive.
		{"FAILED TO RETRIEVE THE ISSUE.", true},
		// Clean plan text — no banned phrases.
		{"Add a new endpoint to the API that returns user profiles.", false},
		{"Fix the nil dereference in the auth middleware.", false},
		{"Refactor the session pool to reduce memory usage.", false},
	}

	for _, tc := range cases {
		phrase, found := validator.ContainsBannedPhrase(tc.text)
		if found != tc.found {
			t.Errorf("ContainsBannedPhrase(%q): got found=%v phrase=%q, want found=%v", tc.text, found, phrase, tc.found)
		}
	}
}

func TestCountTokenOverlap(t *testing.T) {
	a := "authentication middleware refactoring session"
	b := "refactoring the authentication layer in session handling"
	overlap := validator.CountTokenOverlap(a, b, 8)
	// "authentication", "refactoring", "middleware" (len<8? no: 10), "session" (7 < 8)
	// tokens >=8: "authentication"(14), "refactoring"(11), "middleware"(10) from a
	// tokens >=8: "refactoring"(11), "authentication"(14), "handling"(8) from b
	// overlap: "authentication", "refactoring" = 2
	if overlap < 2 {
		t.Errorf("CountTokenOverlap: expected >= 2, got %d", overlap)
	}

	// No overlap at all.
	zero := validator.CountTokenOverlap("apples oranges", "something completely different", 8)
	if zero != 0 {
		t.Errorf("CountTokenOverlap: expected 0 for disjoint texts, got %d", zero)
	}
}

const validReadyPlan = `{
	"issue_number": 42,
	"revision": 1,
	"plan_markdown": "Add a retry mechanism to the authentication middleware to handle transient database timeouts.",
	"goal_summary": "Improve resilience of the authentication layer.",
	"executive_summary": "Users will see fewer login failures during database hiccups.",
	"files_to_modify": [],
	"dependencies_detected": [],
	"questions": [],
	"estimated_complexity": "S",
	"ready_to_execute": true
}`

const bannedPhraseReadyPlan = `{
	"issue_number": 42,
	"revision": 1,
	"plan_markdown": "I couldn't fetch the issue body so I am guessing at the requirements.",
	"goal_summary": "Unknown — I could not read the issue.",
	"executive_summary": "Unknown outcome.",
	"files_to_modify": [],
	"dependencies_detected": [],
	"questions": [],
	"estimated_complexity": "S",
	"ready_to_execute": true
}`

const bannedPhraseNotReadyPlan = `{
	"issue_number": 42,
	"revision": 1,
	"plan_markdown": "I couldn't fetch the issue body — please paste the text.",
	"goal_summary": "",
	"executive_summary": "",
	"files_to_modify": [],
	"dependencies_detected": [],
	"questions": [],
	"estimated_complexity": "S",
	"ready_to_execute": false
}`

func TestValidatePlanContent_CleanReadyPlan(t *testing.T) {
	if err := validator.ValidatePlanContent([]byte(validReadyPlan), 0, ""); err != nil {
		t.Fatalf("expected clean plan to pass, got: %v", err)
	}
}

func TestValidatePlanContent_BannedPhraseReadyPlanRejected(t *testing.T) {
	err := validator.ValidatePlanContent([]byte(bannedPhraseReadyPlan), 0, "")
	if err == nil {
		t.Fatal("expected rejection for banned phrase + ready_to_execute: true, got nil")
	}
}

func TestValidatePlanContent_BannedPhraseNotReadyPlanAllowed(t *testing.T) {
	// Banned phrases are only hard-rejected when ready_to_execute: true.
	// When false, the plan can contain them (the planner is correctly
	// signalling it couldn't fetch the issue and not marking it ready).
	if err := validator.ValidatePlanContent([]byte(bannedPhraseNotReadyPlan), 0, ""); err != nil {
		t.Fatalf("expected plan with banned phrase but ready_to_execute:false to pass, got: %v", err)
	}
}

func TestValidatePlanContent_TokenOverlapSufficient(t *testing.T) {
	issueBody := "authentication middleware refactoring transient database timeouts resilience"
	if err := validator.ValidatePlanContent([]byte(validReadyPlan), 3, issueBody); err != nil {
		t.Fatalf("expected sufficient overlap to pass, got: %v", err)
	}
}

func TestValidatePlanContent_TokenOverlapInsufficient(t *testing.T) {
	issueBody := "completely unrelated topic about something else entirely"
	err := validator.ValidatePlanContent([]byte(validReadyPlan), 3, issueBody)
	if err == nil {
		t.Fatal("expected rejection for insufficient token overlap, got nil")
	}
}

func TestValidatePlanContent_EmptyIssueBodySkipsOverlapCheck(t *testing.T) {
	// When issueBody is empty (gh fetch failed), the overlap check is skipped.
	if err := validator.ValidatePlanContent([]byte(validReadyPlan), 3, ""); err != nil {
		t.Fatalf("expected empty issueBody to skip overlap check, got: %v", err)
	}
}

func TestValidatePlanContent_MalformedJSONReturnsNil(t *testing.T) {
	// Structural errors are caught by planio.ValidatePlanJSON; content
	// validator returns nil so callers can surface the structural error.
	if err := validator.ValidatePlanContent([]byte("{not json"), 0, ""); err != nil {
		t.Fatalf("expected malformed JSON to return nil, got: %v", err)
	}
}
