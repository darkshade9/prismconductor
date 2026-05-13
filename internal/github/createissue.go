package github

import (
	"context"
	"fmt"

	gh "github.com/google/go-github/v62/github"

	"prismconductor/internal/types"
)

// CreateIssue files a new issue on the workspace's GitHub repo. Returns the
// created issue number and HTML URL. Used by the fan-out approval flow (#297).
func (c *Client) CreateIssue(ctx context.Context, ws types.Workspace, title, body string, labels []string) (int, string, error) {
	if ws.GitHubOwner == "" || ws.GitHubRepo == "" {
		return 0, "", fmt.Errorf("workspace %q missing github_owner / github_repo", ws.ID)
	}
	req := &gh.IssueRequest{
		Title: gh.String(title),
		Body:  gh.String(body),
	}
	if len(labels) > 0 {
		req.Labels = &labels
	}
	iss, _, err := c.api.Issues.Create(ctx, ws.GitHubOwner, ws.GitHubRepo, req)
	if err != nil {
		return 0, "", err
	}
	return iss.GetNumber(), iss.GetHTMLURL(), nil
}
