package planio

import (
	"strings"
	"unicode"
)

// DefaultOverlapThreshold is the advisory minimum fraction of unique issue
// tokens that must appear in plan_markdown (issue #197, Phase 2).
const DefaultOverlapThreshold = 0.20

// TokenOverlapScore returns the fraction of unique tokens from issueText that
// also appear in planMarkdown. Used as an advisory relevance check: a score
// below DefaultOverlapThreshold suggests the plan did not engage with the issue.
//
// Tokens are lowercase, alphabetic/digit runs of 4+ characters. This cheap
// filter removes most function words without needing a stopword list.
//
// Returns 1.0 when issueText has no qualifying tokens so callers don't block
// on issues with empty or whitespace-only bodies.
func TokenOverlapScore(issueText, planMarkdown string) float64 {
	issueToks := tokenSet(issueText)
	if len(issueToks) == 0 {
		return 1.0
	}
	planToks := tokenSet(planMarkdown)
	var hits int
	for tok := range issueToks {
		if planToks[tok] {
			hits++
		}
	}
	return float64(hits) / float64(len(issueToks))
}

func tokenSet(text string) map[string]bool {
	toks := make(map[string]bool)
	for _, w := range strings.FieldsFunc(strings.ToLower(text), func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsDigit(r)
	}) {
		if len(w) >= 4 {
			toks[w] = true
		}
	}
	return toks
}
