// Package handlers contains helper logic for specific event-bus handlers that
// would otherwise bloat app.go (issue #124).
package handlers

import (
	"fmt"

	"prismconductor/internal/types"
)

// BuildManualPushCommand returns the exact shell command the user should paste
// to complete a Tier 3 commit+push from the preserved worktree (issue #175).
//
// For commit_signing kind (commit never landed): stages, commits from the
// worker-drafted commit-msg file using git commit -S -F so shell metacharacters
// in the message don't corrupt the clipboard string, then pushes.
//
// For push kind (commit landed locally but remote push was rejected): only pushes.
func BuildManualPushCommand(info *types.NeedsPRInfo) string {
	if info == nil {
		return ""
	}
	switch info.Kind {
	case "commit_signing":
		if info.CommitMsgFile != "" {
			return fmt.Sprintf(
				"cd %q && git add -A && git commit -S -F %q && git push -u origin %s",
				info.WorktreeDir, info.CommitMsgFile, info.Branch,
			)
		}
		// Commit msg file not present: fall back to a placeholder message.
		return fmt.Sprintf(
			"cd %q && git add -A && git commit -S -m 'feat: implement changes' && git push -u origin %s",
			info.WorktreeDir, info.Branch,
		)
	default: // "push" or empty
		return fmt.Sprintf(
			"cd %q && git push -u origin %s",
			info.WorktreeDir, info.Branch,
		)
	}
}
