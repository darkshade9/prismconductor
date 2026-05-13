package session

import (
	"strings"
	"testing"

	"prismconductor/internal/types"
)

func TestIssueBranch_GitHub(t *testing.T) {
	ws := types.Workspace{TrackerKind: "github"}
	iss := types.Issue{Number: 42, Title: "Fix the bug"}
	branch := issueBranch(ws, iss)
	if !strings.HasPrefix(branch, "feat/issue-42-") {
		t.Errorf("expected feat/issue-42- prefix, got %q", branch)
	}
}

func TestIssueBranch_DefaultIsGitHub(t *testing.T) {
	// Empty TrackerKind falls back to GitHub format.
	ws := types.Workspace{}
	iss := types.Issue{Number: 7, Title: "Add feature"}
	branch := issueBranch(ws, iss)
	if !strings.HasPrefix(branch, "feat/issue-7-") {
		t.Errorf("expected feat/issue-7- prefix, got %q", branch)
	}
}

func TestIssueBranch_Jira(t *testing.T) {
	ws := types.Workspace{TrackerKind: "jira"}
	iss := types.Issue{
		Number: 1001,
		Title:  "Add dark mode",
		TrackerRef: &types.TrackerRef{
			Kind:       "jira",
			Identifier: "PROJ-123",
		},
	}
	branch := issueBranch(ws, iss)
	if !strings.HasPrefix(branch, "feat/proj-123-") {
		t.Errorf("expected feat/proj-123- prefix, got %q", branch)
	}
	if !strings.Contains(branch, "add-dark-mode") {
		t.Errorf("expected slug in branch, got %q", branch)
	}
}

func TestIssueBranch_JiraNoTrackerRef(t *testing.T) {
	// Jira workspace but issue has no TrackerRef yet — falls back to numeric.
	ws := types.Workspace{TrackerKind: "jira"}
	iss := types.Issue{Number: 999, Title: "Orphan issue"}
	branch := issueBranch(ws, iss)
	if !strings.HasPrefix(branch, "feat/issue-999-") {
		t.Errorf("expected feat/issue-999- prefix, got %q", branch)
	}
}
