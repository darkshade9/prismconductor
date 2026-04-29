// Package github wraps the GitHub API client and the per-workspace poller (PRISMCONDUCTOR_PLAN.md §15.2).
package github

import (
	"context"
	"fmt"
	"os/exec"
	"strings"

	gh "github.com/google/go-github/v62/github"

	"prismconductor/internal/types"
)

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
