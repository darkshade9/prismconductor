// Package validator provides content-relevance checks for conductor plan files
// (issue #232). These checks are layered on top of the structural validation
// in internal/planio and enforce that the planner actually read the GitHub issue
// before claiming the plan is ready to execute.
package validator

import (
	"encoding/json"
	"fmt"
	"strings"
	"unicode"
)

// BannedPhrases are lowercase substrings that indicate the planner failed to
// fetch the GitHub issue body. A plan with ready_to_execute: true that
// contains any of these phrases must be rejected.
var BannedPhrases = []string{
	"i couldn't fetch",
	"i could not fetch",
	"failed to retrieve the issue",
	"could not access the issue",
	"unable to fetch the issue",
	"i was unable to read",
	"i could not read the issue",
	"i couldn't read the issue",
	"failed to fetch the issue",
	"cannot fetch the issue",
	"unable to retrieve the issue",
	"couldn't access the issue",
}

// ContainsBannedPhrase returns the first matching banned phrase and true when
// the lowercased text matches any entry in BannedPhrases.
func ContainsBannedPhrase(text string) (string, bool) {
	lower := strings.ToLower(text)
	for _, phrase := range BannedPhrases {
		if strings.Contains(lower, phrase) {
			return phrase, true
		}
	}
	return "", false
}

// longTokens returns the set of unique lowercased words of length >= minLen
// found in text, stripping punctuation.
func longTokens(text string, minLen int) map[string]struct{} {
	out := make(map[string]struct{})
	for _, word := range strings.FieldsFunc(strings.ToLower(text), func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsDigit(r)
	}) {
		if len(word) >= minLen {
			out[word] = struct{}{}
		}
	}
	return out
}

// CountTokenOverlap returns the number of distinct tokens of length >= minLen
// that appear in both a and b.
func CountTokenOverlap(a, b string, minLen int) int {
	tokensA := longTokens(a, minLen)
	tokensB := longTokens(b, minLen)
	count := 0
	for tok := range tokensA {
		if _, ok := tokensB[tok]; ok {
			count++
		}
	}
	return count
}

// planContent is the subset of plan fields checked for content relevance.
type planContent struct {
	ReadyToExecute   bool   `json:"ready_to_execute"`
	PlanMarkdown     string `json:"plan_markdown"`
	GoalSummary      string `json:"goal_summary"`
	ExecutiveSummary string `json:"executive_summary"`
}

// ValidatePlanContent checks a plan JSON document for content-relevance issues.
//
// It rejects any plan with ready_to_execute: true that contains a banned
// fetch-failure phrase. When issueBody is non-empty and ready_to_execute is
// true, it also requires that at least minOverlap distinct long tokens (length
// >= 8) appear in both the plan and the issue body.
//
// Structural errors in planJSON are ignored — callers should run
// planio.ValidatePlanJSON first.
func ValidatePlanContent(planJSON []byte, minOverlap int, issueBody string) error {
	var p planContent
	if err := json.Unmarshal(planJSON, &p); err != nil {
		return nil
	}

	combined := strings.Join([]string{p.PlanMarkdown, p.GoalSummary, p.ExecutiveSummary}, " ")

	if phrase, found := ContainsBannedPhrase(combined); found {
		if p.ReadyToExecute {
			return fmt.Errorf("plan is marked ready_to_execute but contains fetch-failure marker %q — set ready_to_execute: false when the issue body could not be fetched", phrase)
		}
	}

	if issueBody != "" && p.ReadyToExecute && minOverlap > 0 {
		overlap := CountTokenOverlap(combined, issueBody, 8)
		if overlap < minOverlap {
			return fmt.Errorf("plan has insufficient token overlap with the issue body (%d of %d required distinct long tokens match) — verify the issue fetch succeeded before marking ready_to_execute: true", overlap, minOverlap)
		}
	}

	return nil
}
