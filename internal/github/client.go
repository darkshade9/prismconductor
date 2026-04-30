// Package github wraps the GitHub API client and the per-workspace poller (PRISMCONDUCTOR_PLAN.md §15.2).
package github

import (
	"context"
	"fmt"
	"os/exec"
	"regexp"
	"strings"
	"time"

	gh "github.com/google/go-github/v62/github"

	"prismconductor/internal/types"
)

var hexColorRe = regexp.MustCompile(`^[0-9a-f]{6}$`)

type Client struct {
	api *gh.Client
}

// New returns a client authenticated via `gh auth token`. Refreshes on each
// New(); callers can call Refresh later if a 401 surfaces.
func New() (*Client, error) {
	tok, err := authToken()
	if err != nil {
		return nil, fmt.Errorf("gh auth token: %w (is `gh` CLI authenticated?)", err)
	}
	return &Client{api: gh.NewClient(nil).WithAuthToken(tok)}, nil
}

func authToken() (string, error) {
	out, err := exec.Command("gh", "auth", "token").Output()
	if err != nil {
		return "", err
	}
	tok := strings.TrimSpace(string(out))
	if tok == "" {
		return "", fmt.Errorf("empty token from `gh auth token`")
	}
	return tok, nil
}

// FetchOpenIssues returns every open issue (excluding pull requests) for a
// workspace's GitHub repo. Paginated at 100/page.
func (c *Client) FetchOpenIssues(ctx context.Context, ws types.Workspace) ([]types.Issue, error) {
	if ws.GitHubOwner == "" || ws.GitHubRepo == "" {
		return nil, fmt.Errorf("workspace %q missing github_owner / github_repo", ws.ID)
	}
	opts := &gh.IssueListByRepoOptions{
		State:       "open",
		ListOptions: gh.ListOptions{PerPage: 100},
	}
	var out []types.Issue
	for {
		page, resp, err := c.api.Issues.ListByRepo(ctx, ws.GitHubOwner, ws.GitHubRepo, opts)
		if err != nil {
			return nil, err
		}
		for _, iss := range page {
			if iss.IsPullRequest() {
				continue
			}
			labels := make([]string, 0, len(iss.Labels))
			for _, l := range iss.Labels {
				labels = append(labels, l.GetName())
			}
			out = append(out, types.Issue{
				Number:      iss.GetNumber(),
				WorkspaceID: ws.ID,
				Title:       iss.GetTitle(),
				Body:        iss.GetBody(),
				Labels:      labels,
				State:       iss.GetState(),
				URL:         iss.GetHTMLURL(),
				UpdatedAt:   iss.GetUpdatedAt().Time,
				Column:      types.ColTodo,
			})
		}
		if resp.NextPage == 0 {
			break
		}
		opts.Page = resp.NextPage
	}
	return out, nil
}

// FetchIssueDetail fetches a single issue by number. Callers are expected to cache the result.
func (c *Client) FetchIssueDetail(ctx context.Context, ws types.Workspace, number int) (*types.Issue, error) {
	if ws.GitHubOwner == "" || ws.GitHubRepo == "" {
		return nil, fmt.Errorf("workspace %q missing github_owner / github_repo", ws.ID)
	}
	iss, _, err := c.api.Issues.Get(ctx, ws.GitHubOwner, ws.GitHubRepo, number)
	if err != nil {
		return nil, err
	}
	labels := make([]string, 0, len(iss.Labels))
	for _, l := range iss.Labels {
		labels = append(labels, l.GetName())
	}
	out := &types.Issue{
		Number:      iss.GetNumber(),
		WorkspaceID: ws.ID,
		Title:       iss.GetTitle(),
		Body:        iss.GetBody(),
		Labels:      labels,
		State:       iss.GetState(),
		URL:         iss.GetHTMLURL(),
		UpdatedAt:   iss.GetUpdatedAt().Time,
	}
	return out, nil
}

// ListLabels returns every label defined on the workspace's GitHub repo.
func (c *Client) ListLabels(ctx context.Context, ws types.Workspace) ([]types.Label, error) {
	if ws.GitHubOwner == "" || ws.GitHubRepo == "" {
		return nil, fmt.Errorf("workspace %q missing github_owner / github_repo", ws.ID)
	}
	opts := &gh.ListOptions{PerPage: 100}
	var out []types.Label
	for {
		page, resp, err := c.api.Issues.ListLabels(ctx, ws.GitHubOwner, ws.GitHubRepo, opts)
		if err != nil {
			return nil, err
		}
		for _, l := range page {
			out = append(out, types.Label{
				Name:        l.GetName(),
				Color:       strings.ToLower(l.GetColor()),
				Description: l.GetDescription(),
			})
		}
		if resp.NextPage == 0 {
			break
		}
		opts.Page = resp.NextPage
	}
	return out, nil
}

func validateLabelColor(c string) error {
	if !hexColorRe.MatchString(c) {
		return fmt.Errorf("color must be 6 lowercase hex chars (got %q)", c)
	}
	return nil
}

// CreateLabel adds a label on the workspace's GitHub repo.
func (c *Client) CreateLabel(ctx context.Context, ws types.Workspace, label types.Label) (types.Label, error) {
	if ws.GitHubOwner == "" || ws.GitHubRepo == "" {
		return types.Label{}, fmt.Errorf("workspace %q missing github_owner / github_repo", ws.ID)
	}
	color := strings.ToLower(strings.TrimPrefix(label.Color, "#"))
	if err := validateLabelColor(color); err != nil {
		return types.Label{}, err
	}
	in := &gh.Label{
		Name:        gh.String(label.Name),
		Color:       gh.String(color),
		Description: gh.String(label.Description),
	}
	got, _, err := c.api.Issues.CreateLabel(ctx, ws.GitHubOwner, ws.GitHubRepo, in)
	if err != nil {
		return types.Label{}, err
	}
	return types.Label{
		Name:        got.GetName(),
		Color:       strings.ToLower(got.GetColor()),
		Description: got.GetDescription(),
	}, nil
}

// UpdateLabel renames / recolors / re-describes an existing label. originalName
// is the current name on GitHub; patch.Name is the new name (may match).
func (c *Client) UpdateLabel(ctx context.Context, ws types.Workspace, originalName string, patch types.Label) (types.Label, error) {
	if ws.GitHubOwner == "" || ws.GitHubRepo == "" {
		return types.Label{}, fmt.Errorf("workspace %q missing github_owner / github_repo", ws.ID)
	}
	color := strings.ToLower(strings.TrimPrefix(patch.Color, "#"))
	if err := validateLabelColor(color); err != nil {
		return types.Label{}, err
	}
	in := &gh.Label{
		Name:        gh.String(patch.Name),
		Color:       gh.String(color),
		Description: gh.String(patch.Description),
	}
	got, _, err := c.api.Issues.EditLabel(ctx, ws.GitHubOwner, ws.GitHubRepo, originalName, in)
	if err != nil {
		return types.Label{}, err
	}
	return types.Label{
		Name:        got.GetName(),
		Color:       strings.ToLower(got.GetColor()),
		Description: got.GetDescription(),
	}, nil
}

// DeleteLabel removes a label on the workspace's GitHub repo.
func (c *Client) DeleteLabel(ctx context.Context, ws types.Workspace, name string) error {
	if ws.GitHubOwner == "" || ws.GitHubRepo == "" {
		return fmt.Errorf("workspace %q missing github_owner / github_repo", ws.ID)
	}
	_, err := c.api.Issues.DeleteLabel(ctx, ws.GitHubOwner, ws.GitHubRepo, name)
	return err
}

// PRState is the slice of GitHub PR fields the poller uses to drive
// REVIEW→DONE on merge and chip-clear on close-without-merge (issue #33).
type PRState struct {
	State    string
	MergedAt *time.Time
	ClosedAt *time.Time
}

// FetchPRState fetches the current state of a single PR. Used by the poller
// once per REVIEW-column issue per tick (rate-bounded; ≤ board-size of REVIEW
// cards on a typical board).
func (c *Client) FetchPRState(ctx context.Context, ws types.Workspace, prNumber int) (*PRState, error) {
	if ws.GitHubOwner == "" || ws.GitHubRepo == "" {
		return nil, fmt.Errorf("workspace %q missing github_owner / github_repo", ws.ID)
	}
	pr, _, err := c.api.PullRequests.Get(ctx, ws.GitHubOwner, ws.GitHubRepo, prNumber)
	if err != nil {
		return nil, err
	}
	out := &PRState{State: pr.GetState()}
	if pr.MergedAt != nil {
		ts := pr.MergedAt.Time
		out.MergedAt = &ts
	}
	if pr.ClosedAt != nil {
		ts := pr.ClosedAt.Time
		out.ClosedAt = &ts
	}
	return out, nil
}

// SetIssueLabels replaces an issue's labels with the given set.
func (c *Client) SetIssueLabels(ctx context.Context, ws types.Workspace, issueNumber int, names []string) error {
	if ws.GitHubOwner == "" || ws.GitHubRepo == "" {
		return fmt.Errorf("workspace %q missing github_owner / github_repo", ws.ID)
	}
	if names == nil {
		names = []string{}
	}
	_, _, err := c.api.Issues.ReplaceLabelsForIssue(ctx, ws.GitHubOwner, ws.GitHubRepo, issueNumber, names)
	return err
}
