// Package handlers contains helper logic for specific event-bus handlers that
// would otherwise bloat app.go (issue #124).
package handlers

import (
	"fmt"
	"strings"

	"prismconductor/internal/issueview"
)

// BuildConflictResolveNote constructs the Continue-Work note for the
// conflict-resolution worker (conductor-resolve-conflicts skill).
// The note describes the conflict context and instructs the worker to
// rebase the branch, resolve conflicts conservatively, and force-push.
func BuildConflictResolveNote(info *issueview.ConflictsInfo) string {
	var b strings.Builder
	fmt.Fprintf(&b, "MERGE CONFLICT: PR branch %q has conflicts with base branch %q.\n\n", info.Head, info.Base)
	if len(info.ConflictingFiles) > 0 {
		b.WriteString("Files changed by this PR (inspect these for conflicts after rebase):\n\n")
		for _, f := range info.ConflictingFiles {
			fmt.Fprintf(&b, "  - %s\n", f)
		}
		b.WriteString("\n")
	}
	b.WriteString("Resolution runbook:\n")
	b.WriteString("1. Fetch the latest base branch: git fetch origin\n")
	fmt.Fprintf(&b, "2. Rebase this branch onto base: git rebase origin/%s\n", info.Base)
	b.WriteString("3. For each conflict: resolve conservatively (prefer upstream intent, keep PR changes where safe)\n")
	b.WriteString("4. Run linters and tests to verify correctness\n")
	b.WriteString("5. Force-push the resolved branch: git push --force-with-lease\n")
	b.WriteString("\nIf a file cannot be safely resolved, emit BLOCKED: <filename> — <reason> and stop.\n")
	return b.String()
}
