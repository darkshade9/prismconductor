// Package tracker defines the Tracker interface and shared types for pluggable
// issue-tracker backends (GitHub, Jira, Linear, GitLab, …).
package tracker

import (
	"context"

	"prismconductor/internal/types"
)

// TrackerKind identifies a tracker backend.
type TrackerKind string

const (
	KindGitHub TrackerKind = "github"
	KindJira   TrackerKind = "jira"
)

// IssueRef is a canonical, tracker-agnostic identifier for a single issue.
type IssueRef struct {
	// Kind is the tracker kind (e.g. "github", "jira").
	Kind TrackerKind `json:"kind"`
	// Identifier is the human-readable key used in branch names and comments.
	// For GitHub: "owner/repo#42". For Jira: "PROJ-123".
	Identifier string `json:"identifier"`
}

// IssueStatus is a named status as defined by the tracker backend.
// For GitHub this maps to "open"/"closed". For Jira it is a status name like
// "In Progress" or "Done".
type IssueStatus struct {
	// ID is the backend-specific status identifier (e.g. a Jira transition ID).
	ID string `json:"id"`
	// Name is the human-readable label shown in the UI.
	Name string `json:"name"`
}

// Tracker is the pluggable issue-tracker interface. Each backend (GitHub,
// Jira, …) implements this interface. The conductor's poller, orchestrator,
// and execute workers call only these methods and are otherwise tracker-agnostic.
type Tracker interface {
	// Kind returns the backend identifier.
	Kind() TrackerKind

	// ListIssues returns all open issues visible to the workspace credentials.
	ListIssues(ctx context.Context, ws types.Workspace) ([]types.Issue, error)

	// FetchIssue returns a single issue by its tracker-specific reference.
	FetchIssue(ctx context.Context, ws types.Workspace, ref IssueRef) (types.Issue, error)

	// UpdateIssueStatus transitions an issue to the given status.
	UpdateIssueStatus(ctx context.Context, ws types.Workspace, ref IssueRef, status IssueStatus) error

	// PostComment adds a text comment to an issue.
	PostComment(ctx context.Context, ws types.Workspace, ref IssueRef, body string) error

	// LinkPRToIssue records the PR URL on the tracker issue (as a comment,
	// remote link, or native PR association, depending on the backend).
	LinkPRToIssue(ctx context.Context, ws types.Workspace, ref IssueRef, prURL string) error

	// ListAvailableStatuses returns the statuses / workflow transitions that
	// can be applied to an issue. Used to populate the workflow mapping UI.
	ListAvailableStatuses(ctx context.Context, ws types.Workspace, ref IssueRef) ([]IssueStatus, error)

	// ListLabels returns every label defined in the workspace. Returns nil for
	// trackers that do not support labels.
	ListLabels(ctx context.Context, ws types.Workspace) ([]string, error)

	// SetIssueLabels replaces the label set on an issue. No-op for trackers
	// that do not support labels.
	SetIssueLabels(ctx context.Context, ws types.Workspace, ref IssueRef, labels []string) error
}
