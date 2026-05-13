// Package github implements the tracker.Tracker interface by wrapping the
// existing internal/github client. Existing GitHub-specific polling and PR
// flows continue to live in internal/github; this adapter is a thin shim that
// satisfies the tracker.Tracker contract so the conductor can select a tracker
// by kind at runtime.
package github

import (
	"context"
	"fmt"

	pcgithub "prismconductor/internal/github"
	"prismconductor/internal/tracker"
	"prismconductor/internal/types"
)

// Adapter wraps *pcgithub.Client and implements tracker.Tracker.
type Adapter struct {
	client *pcgithub.Client
}

// New returns an Adapter backed by the given GitHub client.
func New(c *pcgithub.Client) *Adapter { return &Adapter{client: c} }

func (a *Adapter) Kind() tracker.TrackerKind { return tracker.KindGitHub }

func (a *Adapter) ListIssues(ctx context.Context, ws types.Workspace) ([]types.Issue, error) {
	if a.client == nil {
		return nil, fmt.Errorf("github tracker: client unavailable")
	}
	return a.client.FetchOpenIssues(ctx, ws)
}

func (a *Adapter) FetchIssue(ctx context.Context, ws types.Workspace, ref tracker.IssueRef) (types.Issue, error) {
	if a.client == nil {
		return types.Issue{}, fmt.Errorf("github tracker: client unavailable")
	}
	num, err := issueNumberFromRef(ref)
	if err != nil {
		return types.Issue{}, err
	}
	iss, err := a.client.FetchIssueDetail(ctx, ws, num)
	if err != nil {
		return types.Issue{}, err
	}
	return *iss, nil
}

func (a *Adapter) UpdateIssueStatus(ctx context.Context, ws types.Workspace, ref tracker.IssueRef, status tracker.IssueStatus) error {
	// GitHub does not support arbitrary status transitions via the Issues API.
	// open/closed state changes are not needed by the conductor-execute flow.
	return nil
}

func (a *Adapter) PostComment(ctx context.Context, ws types.Workspace, ref tracker.IssueRef, body string) error {
	if a.client == nil {
		return fmt.Errorf("github tracker: client unavailable")
	}
	num, err := issueNumberFromRef(ref)
	if err != nil {
		return err
	}
	_, err = a.client.PostIssueComment(ctx, ws, num, body)
	return err
}

func (a *Adapter) LinkPRToIssue(ctx context.Context, ws types.Workspace, ref tracker.IssueRef, prURL string) error {
	// GitHub automatically links PRs that contain "Closes #N" in the body.
	// No separate API call is required here.
	return nil
}

func (a *Adapter) ListAvailableStatuses(_ context.Context, _ types.Workspace, _ tracker.IssueRef) ([]tracker.IssueStatus, error) {
	return []tracker.IssueStatus{
		{ID: "open", Name: "Open"},
		{ID: "closed", Name: "Closed"},
	}, nil
}

func (a *Adapter) ListLabels(ctx context.Context, ws types.Workspace) ([]string, error) {
	if a.client == nil {
		return nil, fmt.Errorf("github tracker: client unavailable")
	}
	labels, err := a.client.ListLabels(ctx, ws)
	if err != nil {
		return nil, err
	}
	out := make([]string, len(labels))
	for i, l := range labels {
		out[i] = l.Name
	}
	return out, nil
}

func (a *Adapter) SetIssueLabels(ctx context.Context, ws types.Workspace, ref tracker.IssueRef, labels []string) error {
	if a.client == nil {
		return fmt.Errorf("github tracker: client unavailable")
	}
	num, err := issueNumberFromRef(ref)
	if err != nil {
		return err
	}
	return a.client.SetIssueLabels(ctx, ws, num, labels)
}

// issueNumberFromRef parses the issue number from a GitHub IssueRef identifier.
// GitHub identifiers have the form "owner/repo#42".
func issueNumberFromRef(ref tracker.IssueRef) (int, error) {
	var num int
	_, err := fmt.Sscanf(ref.Identifier[indexOf(ref.Identifier, '#')+1:], "%d", &num)
	if err != nil || num <= 0 {
		return 0, fmt.Errorf("github tracker: cannot parse issue number from ref %q", ref.Identifier)
	}
	return num, nil
}

func indexOf(s string, b byte) int {
	for i := 0; i < len(s); i++ {
		if s[i] == b {
			return i
		}
	}
	return len(s) - 1
}
