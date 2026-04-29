// Package goalfilter applies a Goal's IssueQuery to a list of issues
// (PRISMCONDUCTOR_PLAN.md §6.2).
package goalfilter

import (
	"strings"

	"prismconductor/internal/types"
)

// Apply returns the subset of issues that match the query. If no labels,
// milestone, or free_text are set, all issues match (modulo includes/excludes).
func Apply(q types.IssueQuery, issues []types.Issue) []types.Issue {
	excludes := intset(q.Excludes)
	includes := intset(q.Includes)

	out := make([]types.Issue, 0, len(issues))
	for _, iss := range issues {
		if excludes[iss.Number] {
			continue
		}
		if includes[iss.Number] {
			out = append(out, iss)
			continue
		}
		if !matchesLabels(q.Labels, iss.Labels) {
			continue
		}
		if q.FreeText != "" && !containsFold(iss.Title+"\n"+iss.Body, q.FreeText) {
			continue
		}
		// Milestone match would require we mirror milestone in the Issue struct.
		// Mirroring is added in Phase 1 Day 3 (#2); for now ignore q.Milestone if set.
		out = append(out, iss)
	}
	return out
}

// matchesLabels: issue must carry every label in q.Labels (AND semantics).
// Empty q.Labels = any issue passes.
func matchesLabels(want, have []string) bool {
	if len(want) == 0 {
		return true
	}
	hset := make(map[string]struct{}, len(have))
	for _, l := range have {
		hset[strings.ToLower(l)] = struct{}{}
	}
	for _, l := range want {
		if _, ok := hset[strings.ToLower(l)]; !ok {
			return false
		}
	}
	return true
}

func containsFold(haystack, needle string) bool {
	return strings.Contains(strings.ToLower(haystack), strings.ToLower(needle))
}

func intset(in []int) map[int]bool {
	out := make(map[int]bool, len(in))
	for _, n := range in {
		out[n] = true
	}
	return out
}
