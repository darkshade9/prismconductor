package orchestrator

import (
	"fmt"

	"prismconductor/internal/types"
)

// CrossDepLoader is the minimal store interface needed to resolve cross-workspace
// issue dependencies. Satisfied by *store.Store.
type CrossDepLoader interface {
	LoadIssue(workspaceID string, number int) (types.Issue, error)
}

// IsCrossDepResolved reports whether a single cross-workspace dependency is
// considered resolved. A dep is resolved when the target issue has moved to the
// "done" column or has been closed (ClosedAt non-nil). Issues still in-flight
// (todo → plan → in_progress → review) are not yet resolved.
func IsCrossDepResolved(store CrossDepLoader, dep types.IssueDep) (bool, error) {
	if store == nil {
		return false, fmt.Errorf("dependency_manager: store is nil")
	}
	iss, err := store.LoadIssue(dep.WorkspaceID, dep.Number)
	if err != nil {
		// If the issue can't be found, treat as unresolved — the dep may be in a
		// workspace not yet synced, or the number is wrong.
		return false, fmt.Errorf("dependency_manager: LoadIssue(%s, %d): %w", dep.WorkspaceID, dep.Number, err)
	}
	return iss.Column == types.ColDone || iss.ClosedAt != nil, nil
}

// AllCrossDepsResolved returns true when every cross-workspace dep on the given
// issue is resolved. If any single dep cannot be loaded, the error is returned
// and the issue is treated as blocked to be safe.
func AllCrossDepsResolved(store CrossDepLoader, deps []types.IssueDep) (bool, error) {
	for _, dep := range deps {
		ok, err := IsCrossDepResolved(store, dep)
		if err != nil {
			return false, err
		}
		if !ok {
			return false, nil
		}
	}
	return true, nil
}
