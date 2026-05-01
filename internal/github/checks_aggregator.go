package github

import (
	"context"
	"fmt"

	gh "github.com/google/go-github/v62/github"

	"prismconductor/internal/types"
)

// ChecksAggregate summarizes the check-run state for a single commit SHA.
type ChecksAggregate struct {
	HeadSHA     string
	AnyFailed   bool
	FailingJobs []FailingJob
}

// FailingJob is a single failing check-run entry.
type FailingJob struct {
	Name  string
	URL   string
	RunID int64
}

// FetchChecksAggregate retrieves GitHub Actions check runs for a commit SHA and
// returns an aggregate. Only check runs whose App.Slug is "github-actions" are
// considered — this filters out third-party status checks that the worker cannot
// fix (issue #116, q3).
//
// A check run is "failing" when its conclusion is one of: failure, timed_out,
// action_required, startup_failure. In-progress runs (conclusion == "") are
// excluded so we only react to definitive results.
func (c *Client) FetchChecksAggregate(ctx context.Context, ws types.Workspace, sha string) (*ChecksAggregate, error) {
	if ws.GitHubOwner == "" || ws.GitHubRepo == "" {
		return nil, fmt.Errorf("workspace %q missing github_owner / github_repo", ws.ID)
	}
	opts := &gh.ListCheckRunsOptions{
		ListOptions: gh.ListOptions{PerPage: 100},
	}
	agg := &ChecksAggregate{HeadSHA: sha}
	for {
		page, resp, err := c.api.Checks.ListCheckRunsForRef(ctx, ws.GitHubOwner, ws.GitHubRepo, sha, opts)
		if err != nil {
			return nil, fmt.Errorf("list check runs for %s: %w", sha, err)
		}
		for _, run := range page.CheckRuns {
			if run.GetApp().GetSlug() != "github-actions" {
				continue
			}
			conclusion := run.GetConclusion()
			switch conclusion {
			case "failure", "timed_out", "action_required", "startup_failure":
				agg.AnyFailed = true
				agg.FailingJobs = append(agg.FailingJobs, FailingJob{
					Name:  run.GetName(),
					URL:   run.GetHTMLURL(),
					RunID: run.GetID(),
				})
			}
		}
		if resp.NextPage == 0 {
			break
		}
		opts.Page = resp.NextPage
	}
	return agg, nil
}
