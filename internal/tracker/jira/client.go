// Package jira implements the tracker.Tracker interface for Jira Cloud (REST
// API v3, email + API token auth). Jira Server / Data Center is out of scope
// for this release (issue #289 Q1 answer: Cloud only).
package jira

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"prismconductor/internal/secretstore"
	"prismconductor/internal/tracker"
	"prismconductor/internal/types"
)

// JiraConfig is the per-workspace Jira configuration stored in
// Workspace.TrackerConfig (json.RawMessage).
type JiraConfig struct {
	// InstanceURL is the Jira Cloud base URL, e.g. "https://myorg.atlassian.net".
	InstanceURL string `json:"instance_url"`
	// Email is the account email used for Basic auth.
	Email string `json:"email"`
	// APIToken is the Atlassian API token. Stored in the OS keyring under the
	// key "prismconductor/jira/<workspaceID>"; the value here may be empty when
	// the token is retrieved at runtime from the keyring.
	APIToken string `json:"api_token,omitempty"`
	// ProjectKey is the Jira project key to poll, e.g. "PROJ".
	ProjectKey string `json:"project_key"`
	// JQL is an optional custom JQL filter appended to the poll query.
	// Defaults to "project = <ProjectKey> AND statusCategory != Done".
	JQL string `json:"jql,omitempty"`
	// WorkflowMapping maps conductor column names to Jira status names.
	// E.g. {"in_progress": "In Progress", "review": "In Review"}.
	WorkflowMapping map[string]string `json:"workflow_mapping,omitempty"`
}

// Client is the Jira Cloud REST API client.
type Client struct {
	cfg        JiraConfig
	httpClient *http.Client
	authHeader string // "Basic <base64(email:token)>"
}

// NewClient creates a new Jira REST API client from the supplied config.
func NewClient(cfg JiraConfig) (*Client, error) {
	if cfg.InstanceURL == "" {
		return nil, fmt.Errorf("jira: instance_url is required")
	}
	if cfg.Email == "" {
		return nil, fmt.Errorf("jira: email is required")
	}
	if cfg.APIToken == "" {
		return nil, fmt.Errorf("jira: api_token is required")
	}
	raw := cfg.Email + ":" + cfg.APIToken
	auth := "Basic " + base64.StdEncoding.EncodeToString([]byte(raw))
	return &Client{
		cfg:        cfg,
		httpClient: &http.Client{Timeout: 30 * time.Second},
		authHeader: auth,
	}, nil
}

// NewClientForWorkspace extracts JiraConfig from ws.TrackerConfig and returns a
// Client. When cfg.APIToken is empty (redacted at persist time), the token is
// retrieved from the OS keyring under "prismconductor/jira/<workspaceID>".
func NewClientForWorkspace(ws types.Workspace) (*Client, error) {
	if ws.TrackerConfig == nil {
		return nil, fmt.Errorf("jira: workspace %q has no tracker_config", ws.ID)
	}
	var cfg JiraConfig
	if err := json.Unmarshal(ws.TrackerConfig, &cfg); err != nil {
		return nil, fmt.Errorf("jira: decode tracker_config for workspace %q: %w", ws.ID, err)
	}
	if cfg.APIToken == "" {
		ss := secretstore.NewKeychainStore()
		tok, err := ss.Get("prismconductor/jira/" + ws.ID)
		if err != nil {
			return nil, fmt.Errorf("jira: api token not found in keyring for workspace %q: %w", ws.ID, err)
		}
		cfg.APIToken = tok
	}
	return NewClient(cfg)
}

// --------------------------------------------------------------------------
// tracker.Tracker implementation
// --------------------------------------------------------------------------

func (c *Client) Kind() tracker.TrackerKind { return tracker.KindJira }

// ListIssues fetches open issues from Jira using the configured JQL.
func (c *Client) ListIssues(ctx context.Context, ws types.Workspace) ([]types.Issue, error) {
	jql := c.effectiveJQL()
	var out []types.Issue
	startAt := 0
	const maxResults = 100
	for {
		page, total, err := c.searchIssues(ctx, jql, startAt, maxResults)
		if err != nil {
			return nil, err
		}
		for _, ji := range page {
			out = append(out, ji.toTypesIssue(ws.ID))
		}
		startAt += len(page)
		if startAt >= total || len(page) == 0 {
			break
		}
	}
	return out, nil
}

// FetchIssue returns a single Jira issue by its key (e.g. "PROJ-123").
func (c *Client) FetchIssue(ctx context.Context, ws types.Workspace, ref tracker.IssueRef) (types.Issue, error) {
	key := ref.Identifier
	ji, err := c.getIssue(ctx, key)
	if err != nil {
		return types.Issue{}, err
	}
	return ji.toTypesIssue(ws.ID), nil
}

// UpdateIssueStatus transitions a Jira issue to the given status (Q2: yes,
// automatically transition per workflow mapping).
func (c *Client) UpdateIssueStatus(ctx context.Context, ws types.Workspace, ref tracker.IssueRef, status tracker.IssueStatus) error {
	if status.ID == "" {
		return fmt.Errorf("jira: status.ID is required for transition")
	}
	return c.doTransition(ctx, ref.Identifier, status.ID)
}

// PostComment posts a plain-text comment on the Jira issue.
func (c *Client) PostComment(ctx context.Context, ws types.Workspace, ref tracker.IssueRef, body string) error {
	return c.createComment(ctx, ref.Identifier, body)
}

// LinkPRToIssue posts the PR URL as a comment and (if a workflow mapping is
// configured for "review") transitions the issue to the review status.
func (c *Client) LinkPRToIssue(ctx context.Context, ws types.Workspace, ref tracker.IssueRef, prURL string) error {
	msg := fmt.Sprintf("Pull request opened: %s", prURL)
	if err := c.createComment(ctx, ref.Identifier, msg); err != nil {
		return err
	}
	// Optionally transition to the review status per workflow mapping.
	if reviewStatus, ok := c.cfg.WorkflowMapping["review"]; ok && reviewStatus != "" {
		transitions, err := c.getTransitions(ctx, ref.Identifier)
		if err == nil {
			for _, t := range transitions {
				if strings.EqualFold(t.Name, reviewStatus) {
					_ = c.doTransition(ctx, ref.Identifier, t.ID)
					break
				}
			}
		}
	}
	return nil
}

// ListAvailableStatuses returns the allowed transitions for the given issue.
func (c *Client) ListAvailableStatuses(ctx context.Context, ws types.Workspace, ref tracker.IssueRef) ([]tracker.IssueStatus, error) {
	transitions, err := c.getTransitions(ctx, ref.Identifier)
	if err != nil {
		return nil, err
	}
	out := make([]tracker.IssueStatus, len(transitions))
	for i, t := range transitions {
		out[i] = tracker.IssueStatus{ID: t.ID, Name: t.Name}
	}
	return out, nil
}

// ListLabels returns Jira labels — the /rest/api/3/label endpoint.
func (c *Client) ListLabels(ctx context.Context, ws types.Workspace) ([]string, error) {
	type labelsResp struct {
		Values []string `json:"values"`
	}
	var resp labelsResp
	if err := c.get(ctx, "/rest/api/3/label", nil, &resp); err != nil {
		return nil, err
	}
	return resp.Values, nil
}

// SetIssueLabels replaces the labels on a Jira issue.
func (c *Client) SetIssueLabels(ctx context.Context, ws types.Workspace, ref tracker.IssueRef, labels []string) error {
	labelNodes := make([]map[string]string, len(labels))
	for i, l := range labels {
		labelNodes[i] = map[string]string{"add": l}
	}
	body := map[string]any{
		"update": map[string]any{
			"labels": labelNodes,
		},
	}
	return c.put(ctx, fmt.Sprintf("/rest/api/3/issue/%s", ref.Identifier), body)
}

// --------------------------------------------------------------------------
// Low-level helpers
// --------------------------------------------------------------------------

// jiraIssue is the minimal Jira issue shape from /rest/api/3/search and
// /rest/api/3/issue/{key}.
type jiraIssue struct {
	ID  string `json:"id"`
	Key string `json:"key"`
	Fields struct {
		Summary string          `json:"summary"`
		Description json.RawMessage `json:"description"` // ADF or plain string
		Status struct {
			Name string `json:"name"`
		} `json:"status"`
		Labels  []string `json:"labels"`
		Updated string   `json:"updated"` // ISO 8601
		Self    string   `json:"self"`
	} `json:"fields"`
}

func (ji *jiraIssue) toTypesIssue(workspaceID string) types.Issue {
	body := ADFToMarkdown(ji.Fields.Description)
	var updatedAt time.Time
	if ji.Fields.Updated != "" {
		t, _ := time.Parse(time.RFC3339, ji.Fields.Updated)
		updatedAt = t
	}
	// Derive a stable numeric stand-in from the Jira issue ID for compatibility
	// with the conductor's int-typed Issue.Number. The Jira "id" field is a
	// numeric string; parse it directly.
	var num int
	fmt.Sscanf(ji.ID, "%d", &num)

	trackerRef := types.TrackerRef{
		Kind:       string(tracker.KindJira),
		Identifier: ji.Key,
	}

	return types.Issue{
		Number:      num,
		WorkspaceID: workspaceID,
		Title:       ji.Fields.Summary,
		Body:        body,
		Labels:      ji.Fields.Labels,
		State:       "open",
		URL:         ji.Fields.Self,
		UpdatedAt:   updatedAt,
		Column:      types.ColTodo,
		TrackerRef:  &trackerRef,
	}
}

type searchResponse struct {
	Total  int          `json:"total"`
	Issues []jiraIssue  `json:"issues"`
}

func (c *Client) searchIssues(ctx context.Context, jql string, startAt, maxResults int) ([]jiraIssue, int, error) {
	params := url.Values{
		"jql":        {jql},
		"startAt":    {fmt.Sprintf("%d", startAt)},
		"maxResults": {fmt.Sprintf("%d", maxResults)},
		"fields":     {"summary,description,status,labels,updated"},
	}
	var resp searchResponse
	if err := c.get(ctx, "/rest/api/3/search", params, &resp); err != nil {
		return nil, 0, err
	}
	return resp.Issues, resp.Total, nil
}

func (c *Client) getIssue(ctx context.Context, key string) (*jiraIssue, error) {
	params := url.Values{
		"fields": {"summary,description,status,labels,updated"},
	}
	var ji jiraIssue
	if err := c.get(ctx, fmt.Sprintf("/rest/api/3/issue/%s", key), params, &ji); err != nil {
		return nil, err
	}
	return &ji, nil
}

type jiraTransition struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

func (c *Client) getTransitions(ctx context.Context, key string) ([]jiraTransition, error) {
	type resp struct {
		Transitions []jiraTransition `json:"transitions"`
	}
	var r resp
	if err := c.get(ctx, fmt.Sprintf("/rest/api/3/issue/%s/transitions", key), nil, &r); err != nil {
		return nil, err
	}
	return r.Transitions, nil
}

func (c *Client) doTransition(ctx context.Context, key, transitionID string) error {
	body := map[string]any{
		"transition": map[string]string{"id": transitionID},
	}
	return c.post(ctx, fmt.Sprintf("/rest/api/3/issue/%s/transitions", key), body, nil)
}

func (c *Client) createComment(ctx context.Context, key, text string) error {
	body := map[string]any{
		"body": map[string]any{
			"version": 1,
			"type":    "doc",
			"content": []any{
				map[string]any{
					"type": "paragraph",
					"content": []any{
						map[string]any{"type": "text", "text": text},
					},
				},
			},
		},
	}
	return c.post(ctx, fmt.Sprintf("/rest/api/3/issue/%s/comment", key), body, nil)
}

// TestConnection verifies credentials by calling /rest/api/3/myself.
func (c *Client) TestConnection(ctx context.Context) error {
	var myself struct {
		AccountID string `json:"accountId"`
	}
	return c.get(ctx, "/rest/api/3/myself", nil, &myself)
}

// TestProject checks that the project key exists and is accessible.
func (c *Client) TestProject(ctx context.Context) error {
	var proj struct {
		Key string `json:"key"`
	}
	return c.get(ctx, fmt.Sprintf("/rest/api/3/project/%s", c.cfg.ProjectKey), nil, &proj)
}

func (c *Client) effectiveJQL() string {
	if c.cfg.JQL != "" {
		return c.cfg.JQL
	}
	return fmt.Sprintf(`project = "%s" AND statusCategory != Done ORDER BY updated DESC`, c.cfg.ProjectKey)
}

func (c *Client) get(ctx context.Context, path string, params url.Values, out any) error {
	u := c.cfg.InstanceURL + path
	if len(params) > 0 {
		u += "?" + params.Encode()
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", c.authHeader)
	req.Header.Set("Accept", "application/json")
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return fmt.Errorf("jira: GET %s → %d %s", path, resp.StatusCode, strings.TrimSpace(string(body)))
	}
	return json.NewDecoder(resp.Body).Decode(out)
}

func (c *Client) post(ctx context.Context, path string, body, out any) error {
	return c.doJSON(ctx, http.MethodPost, path, body, out)
}

func (c *Client) put(ctx context.Context, path string, body any) error {
	return c.doJSON(ctx, http.MethodPut, path, body, nil)
}

func (c *Client) doJSON(ctx context.Context, method, path string, body, out any) error {
	var buf bytes.Buffer
	if body != nil {
		if err := json.NewEncoder(&buf).Encode(body); err != nil {
			return err
		}
	}
	req, err := http.NewRequestWithContext(ctx, method, c.cfg.InstanceURL+path, &buf)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", c.authHeader)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return fmt.Errorf("jira: %s %s → %d %s", method, path, resp.StatusCode, strings.TrimSpace(string(b)))
	}
	if out != nil && resp.StatusCode != http.StatusNoContent {
		return json.NewDecoder(resp.Body).Decode(out)
	}
	return nil
}
