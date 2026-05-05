package planio

import (
	"testing"
)

func TestTokenOverlapScore_FullOverlap(t *testing.T) {
	issue := "authentication token refresh failing on mobile clients"
	plan := "fix authentication token refresh logic for mobile clients only"
	score := TokenOverlapScore(issue, plan)
	if score < DefaultOverlapThreshold {
		t.Fatalf("expected score >= %.2f for full-overlap case, got %.3f", DefaultOverlapThreshold, score)
	}
}

func TestTokenOverlapScore_ZeroOverlap(t *testing.T) {
	issue := "authentication token refresh failing on mobile clients"
	plan := "completely unrelated work on database schema migration"
	score := TokenOverlapScore(issue, plan)
	if score >= DefaultOverlapThreshold {
		t.Fatalf("expected score < %.2f for zero-overlap case, got %.3f", DefaultOverlapThreshold, score)
	}
}

func TestTokenOverlapScore_EmptyIssueReturnsOne(t *testing.T) {
	score := TokenOverlapScore("", "some plan content here")
	if score != 1.0 {
		t.Fatalf("expected 1.0 for empty issue text, got %f", score)
	}
}

func TestTokenOverlapScore_WhitespaceOnlyIssueReturnsOne(t *testing.T) {
	score := TokenOverlapScore("   \n\t  ", "some plan")
	if score != 1.0 {
		t.Fatalf("expected 1.0 for whitespace-only issue, got %f", score)
	}
}

func TestTokenOverlapScore_ShortTokensIgnored(t *testing.T) {
	// All tokens < 4 chars; should return 1.0 (no qualifying tokens).
	score := TokenOverlapScore("a b cc ddd", "xyz abc def")
	if score != 1.0 {
		t.Fatalf("expected 1.0 when only short tokens, got %f", score)
	}
}

func TestTokenOverlapScore_CaseInsensitive(t *testing.T) {
	score := TokenOverlapScore("Authentication FAILURE error", "authentication failure detected")
	if score < DefaultOverlapThreshold {
		t.Fatalf("expected score >= %.2f for same words different case, got %.3f", DefaultOverlapThreshold, score)
	}
}

func TestTokenOverlapScore_ModerateThreshold(t *testing.T) {
	// 5 qualifying issue tokens; plan contains 1 → score = 0.20, exactly at threshold.
	issue := "planner produces wrong plans because issue body missing"
	// Extract 5 qualifying tokens from above: "planner", "produces", "wrong", "plans", "because"
	plan := "planner should read issue before writing"
	score := TokenOverlapScore(issue, plan)
	if score < DefaultOverlapThreshold {
		t.Logf("score=%.3f below threshold=%.2f — may flag as advisory warning", score, DefaultOverlapThreshold)
	}
}

// tokenSet is unexported; test via TokenOverlapScore.
func TestTokenOverlapScore_DeduplicatesTokens(t *testing.T) {
	// Repeated tokens in issue should not inflate denominator.
	issue := "token token token token validation"
	plan := "token validation logic"
	score := TokenOverlapScore(issue, plan)
	// Unique tokens: "token", "validation" → 2 unique; plan has both → score=1.0
	if score != 1.0 {
		t.Fatalf("expected 1.0 when all unique issue tokens appear in plan, got %f", score)
	}
}
