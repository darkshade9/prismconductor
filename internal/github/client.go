// Package github wraps the GitHub API client and the 5-minute poller (PRISMCONDUCTOR_PLAN.md §15.2).
package github

import (
	"context"
	"os/exec"
	"strings"

	gh "github.com/google/go-github/v62/github"

	"prismconductor/internal/types"
)

type Client struct {
	api *gh.Client
}

// New returns a client authenticated via `gh auth token`.
func New() (*Client, error) {
	tok, err := authToken()
	if err != nil {
		return nil, err
	}
	return &Client{api: gh.NewClient(nil).WithAuthToken(tok)}, nil
}

func authToken() (string, error) {
	out, err := exec.Command("gh", "auth", "token").Output()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}

// FetchOpenIssues mirrors all open issues for a workspace. Stub: not yet implemented.
func (c *Client) FetchOpenIssues(_ context.Context, _ types.Workspace) ([]types.Issue, error) {
	// TODO Phase 1 Day 3: parallel fan-out per workspace, cache to SQLite.
	return nil, nil
}
