// Package workers contains helper types for conductor worker sessions (#124).
package workers

// ConflictResolutionStrategy controls how the conflict-resolution worker
// updates the PR branch.
type ConflictResolutionStrategy string

const (
	// StrategyRebase rebases the PR branch onto the base branch, producing a
	// clean linear history. Requires a force-push on success.
	StrategyRebase ConflictResolutionStrategy = "rebase"

	// StrategyMerge creates a merge commit from the base into the PR branch.
	// Safer for history but avoids the need for force-push.
	StrategyMerge ConflictResolutionStrategy = "merge"
)

// ConflictResolverConfig is the configuration passed to the
// conductor-resolve-conflicts skill. The defaults (rebase, no approval)
// match the user's answers from plan rev1 (#124 q1, q2).
type ConflictResolverConfig struct {
	Strategy         ConflictResolutionStrategy `json:"strategy"`
	RequiresApproval bool                       `json:"requires_approval"`
}

// DefaultConfig returns the default ConflictResolverConfig.
// Rebase strategy, no human-approval gate (plan #124 q1=rebase, q2=no).
func DefaultConfig() ConflictResolverConfig {
	return ConflictResolverConfig{
		Strategy:         StrategyRebase,
		RequiresApproval: false,
	}
}
