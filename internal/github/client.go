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
// HeadSHA is added for check-run failure detection (#116).
// MergeableState + Base/HeadRef are added for conflict detection (#124).
type PRState struct {
	State          string
	MergedAt       *time.Time
	ClosedAt       *time.Time
	HeadSHA        string
	MergeableState string // "clean", "dirty", "blocked", "unknown", or ""
	BaseBranch     string
	HeadRef        string
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
	out := &PRState{
		State:          pr.GetState(),
		HeadSHA:        pr.GetHead().GetSHA(),
		MergeableState: pr.GetMergeableState(),
		BaseBranch:     pr.GetBase().GetRef(),
		HeadRef:        pr.GetHead().GetRef(),
	}
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

// FetchPRFiles returns the filenames of every file changed by a pull request.
// Used to populate ConflictingFiles when a PR becomes dirty (#124).
func (c *Client) FetchPRFiles(ctx context.Context, ws types.Workspace, prNumber int) ([]string, error) {
	if ws.GitHubOwner == "" || ws.GitHubRepo == "" {
		return nil, fmt.Errorf("workspace %q missing github_owner / github_repo", ws.ID)
	}
	opts := &gh.ListOptions{PerPage: 100}
	var files []string
	for {
		page, resp, err := c.api.PullRequests.ListFiles(ctx, ws.GitHubOwner, ws.GitHubRepo, prNumber, opts)
		if err != nil {
			return nil, err
		}
		for _, f := range page {
			files = append(files, f.GetFilename())
		}
		if resp.NextPage == 0 {
			break
		}
		opts.Page = resp.NextPage
	}
	return files, nil
}

// FetchOpenPRsForBranch returns the first open PR against the given branch
// (head ref), or (0, "", nil) if none exists. Used by the NEEDS_PR poller to
// auto-attach a PR when the user pushes and opens one manually (#157).
func (c *Client) FetchOpenPRsForBranch(ctx context.Context, ws types.Workspace, branch string) (prNumber int, prURL string, err error) {
	if ws.GitHubOwner == "" || ws.GitHubRepo == "" {
		return 0, "", fmt.Errorf("workspace %q missing github_owner / github_repo", ws.ID)
	}
	opts := &gh.PullRequestListOptions{
		State:       "open",
		Head:        ws.GitHubOwner + ":" + branch,
		ListOptions: gh.ListOptions{PerPage: 1},
	}
	prs, _, err := c.api.PullRequests.List(ctx, ws.GitHubOwner, ws.GitHubRepo, opts)
	if err != nil {
		return 0, "", err
	}
	if len(prs) == 0 {
		return 0, "", nil
	}
	return prs[0].GetNumber(), prs[0].GetHTMLURL(), nil
}

// FetchIssueComments returns conversation-level comments on a GitHub issue
// (the PR issue thread), optionally filtered to only those created after
// `since`. Pass zero time to get all comments. Issue #159.
func (c *Client) FetchIssueComments(ctx context.Context, ws types.Workspace, issueNumber int, since time.Time) ([]types.PRComment, error) {
	if ws.GitHubOwner == "" || ws.GitHubRepo == "" {
		return nil, fmt.Errorf("workspace %q missing github_owner / github_repo", ws.ID)
	}
	opts := &gh.IssueListCommentsOptions{
		ListOptions: gh.ListOptions{PerPage: 50},
	}
	if !since.IsZero() {
		opts.Since = &since
	}
	var out []types.PRComment
	for {
		page, resp, err := c.api.Issues.ListComments(ctx, ws.GitHubOwner, ws.GitHubRepo, issueNumber, opts)
		if err != nil {
			return nil, err
		}
		for _, cm := range page {
			out = append(out, types.PRComment{
				WorkspaceID: ws.ID,
				IssueNumber: issueNumber,
				CommentID:   cm.GetID(),
				Author:      cm.GetUser().GetLogin(),
				Body:        cm.GetBody(),
				Kind:        types.PRCommentKindConversation,
				CreatedAt:   cm.GetCreatedAt().Time,
			})
		}
		if resp.NextPage == 0 {
			break
		}
		opts.Page = resp.NextPage
	}
	return out, nil
}

// FetchPRReviewComments returns inline diff (review) comments on a PR.
// Issue #159.
func (c *Client) FetchPRReviewComments(ctx context.Context, ws types.Workspace, prNumber int, since time.Time) ([]types.PRComment, error) {
	if ws.GitHubOwner == "" || ws.GitHubRepo == "" {
		return nil, fmt.Errorf("workspace %q missing github_owner / github_repo", ws.ID)
	}
	opts := &gh.PullRequestListCommentsOptions{
		ListOptions: gh.ListOptions{PerPage: 50},
	}
	if !since.IsZero() {
		opts.Since = since
	}
	var out []types.PRComment
	for {
		page, resp, err := c.api.PullRequests.ListComments(ctx, ws.GitHubOwner, ws.GitHubRepo, prNumber, opts)
		if err != nil {
			return nil, err
		}
		for _, cm := range page {
			out = append(out, types.PRComment{
				WorkspaceID: ws.ID,
				IssueNumber: prNumber,
				CommentID:   cm.GetID(),
				Author:      cm.GetUser().GetLogin(),
				Body:        cm.GetBody(),
				Kind:        types.PRCommentKindReview,
				FilePath:    cm.GetPath(),
				LineNumber:  cm.GetLine(),
				CreatedAt:   cm.GetCreatedAt().Time,
			})
		}
		if resp.NextPage == 0 {
			break
		}
		opts.Page = resp.NextPage
	}
	return out, nil
}

// PostIssueComment posts a text comment on the GitHub issue (PR thread).
// Returns the newly created comment ID. Issue #159.
func (c *Client) PostIssueComment(ctx context.Context, ws types.Workspace, issueNumber int, body string) (int64, error) {
	if ws.GitHubOwner == "" || ws.GitHubRepo == "" {
		return 0, fmt.Errorf("workspace %q missing github_owner / github_repo", ws.ID)
	}
	cm, _, err := c.api.Issues.CreateComment(ctx, ws.GitHubOwner, ws.GitHubRepo, issueNumber, &gh.IssueComment{
		Body: gh.String(body),
	})
	if err != nil {
		return 0, err
	}
	return cm.GetID(), nil
}

// GetAuthenticatedUser returns the login of the currently authenticated gh CLI
// user. Used to filter out the conductor's own comments from the unread badge.
// Issue #159.
func (c *Client) GetAuthenticatedUser(ctx context.Context) (string, error) {
	u, _, err := c.api.Users.Get(ctx, "")
	if err != nil {
		return "", err
	}
	return u.GetLogin(), nil
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
