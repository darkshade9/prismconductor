// Package remoteworker implements the Cloudflare Worker execution target for
// remote workspaces (issue #171). It provides two concerns:
//
//   - Cloudflare API operations: deploying the worker bundle, creating secrets,
//     verifying tokens.
//   - GitHub PAT verification: checking push capability before persisting the ref.
//
// Raw token values never touch disk; callers store only the CF Secret names
// (RemoteSecretRefs) and the account/worker metadata (RemoteConfig).
package remoteworker

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"strings"
	"time"
)

const (
	cfAPIBase     = "https://api.cloudflare.com/client/v4"
	ghAPIBase     = "https://api.github.com"
	workerNamePfx = "prismconductor"
	secretGHPAT   = "GITHUB_PAT"
)

var httpClient = &http.Client{Timeout: 30 * time.Second}

// TokenVerifyResult is returned by VerifyToken to the App layer.
type TokenVerifyResult struct {
	AccountID string `json:"account_id"`
	AccountName string `json:"account_name,omitempty"`
}

// DeployResult is returned by DeployWorker.
type DeployResult struct {
	WorkerName          string `json:"worker_name"`
	CFWorkerEndpointURL string `json:"cf_worker_endpoint_url"`
	DeploymentVersion   string `json:"deployment_version"`
}

// cfResponse is the envelope shape for Cloudflare v4 API responses.
type cfResponse struct {
	Success  bool            `json:"success"`
	Errors   []cfError       `json:"errors"`
	Result   json.RawMessage `json:"result"`
}

type cfError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

func (e cfError) Error() string { return fmt.Sprintf("CF API %d: %s", e.Code, e.Message) }

func cfDo(method, url, token string, body io.Reader, contentType string) (json.RawMessage, error) {
	req, err := http.NewRequest(method, url, body)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("CF API request: %w", err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	var env cfResponse
	if err := json.Unmarshal(raw, &env); err != nil {
		return nil, fmt.Errorf("CF API parse (%d): %w", resp.StatusCode, err)
	}
	if !env.Success {
		if len(env.Errors) > 0 {
			return nil, env.Errors[0]
		}
		return nil, fmt.Errorf("CF API error (HTTP %d)", resp.StatusCode)
	}
	return env.Result, nil
}

// VerifyToken validates a CF API token and returns the account ID.
// Corresponds to GET /user/tokens/verify + GET /accounts.
func VerifyToken(token string) (TokenVerifyResult, error) {
	// Verify the token itself is valid.
	if _, err := cfDo("GET", cfAPIBase+"/user/tokens/verify", token, nil, ""); err != nil {
		return TokenVerifyResult{}, fmt.Errorf("token invalid: %w", err)
	}
	// Fetch the first account the token can access.
	raw, err := cfDo("GET", cfAPIBase+"/accounts?per_page=1", token, nil, "")
	if err != nil {
		return TokenVerifyResult{}, fmt.Errorf("list accounts: %w", err)
	}
	var accounts []struct {
		ID   string `json:"id"`
		Name string `json:"name"`
	}
	if err := json.Unmarshal(raw, &accounts); err != nil || len(accounts) == 0 {
		return TokenVerifyResult{}, fmt.Errorf("could not resolve account ID from token")
	}
	return TokenVerifyResult{
		AccountID:   accounts[0].ID,
		AccountName: accounts[0].Name,
	}, nil
}

// DeployWorker uploads workerScript (JavaScript source) to the user's CF
// account as a new Worker script named "prismconductor-<workspaceID>" and
// returns the worker's endpoint URL and deployment version tag.
//
// The workerScript bytes are the compiled bundle embedded via go:embed in
// app.go (internal/remoteworker/embed.go).
func DeployWorker(accountID, token, workspaceID string, workerScript []byte) (DeployResult, error) {
	workerName := workerNamePfx + "-" + sanitizeID(workspaceID)

	// Build multipart body: CF Workers API expects metadata + script parts.
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)

	// Bindings are NOT declared here: Cloudflare rejects secret_text bindings
	// that lack a "text" value (API error 10021). UpsertSecret calls after
	// deploy create the bindings atomically with the secret value.
	meta := map[string]any{
		"main_module":        "worker.js",
		"compatibility_date": "2024-01-01",
	}
	metaJSON, _ := json.Marshal(meta)
	mPart, err := mw.CreatePart(map[string][]string{
		"Content-Disposition": {`form-data; name="metadata"`},
		"Content-Type":        {"application/json"},
	})
	if err != nil {
		return DeployResult{}, err
	}
	if _, err := mPart.Write(metaJSON); err != nil {
		return DeployResult{}, err
	}

	sPart, err := mw.CreatePart(map[string][]string{
		"Content-Disposition": {`form-data; name="worker.js"; filename="worker.js"`},
		"Content-Type":        {"application/javascript+module"},
	})
	if err != nil {
		return DeployResult{}, err
	}
	if _, err := sPart.Write(workerScript); err != nil {
		return DeployResult{}, err
	}
	_ = mw.Close()

	url := fmt.Sprintf("%s/accounts/%s/workers/scripts/%s", cfAPIBase, accountID, workerName)
	raw, err := cfDo("PUT", url, token, &buf, mw.FormDataContentType())
	if err != nil {
		return DeployResult{}, fmt.Errorf("deploy worker: %w", err)
	}

	var script struct {
		Etag string `json:"etag"`
	}
	_ = json.Unmarshal(raw, &script)

	endpointURL := fmt.Sprintf("https://%s.workers.dev", workerName)
	return DeployResult{
		WorkerName:          workerName,
		CFWorkerEndpointURL: endpointURL,
		DeploymentVersion:   script.Etag,
	}, nil
}

// UpsertSecret creates or replaces a secret on a deployed CF Worker.
// secretName is one of the names in RemoteSecretRefs; secretValue is the raw
// token that must NOT be persisted locally after this call returns.
func UpsertSecret(accountID, token, workerName, secretName, secretValue string) error {
	payload, _ := json.Marshal(map[string]string{
		"name":  secretName,
		"text":  secretValue,
		"type":  "secret_text",
	})
	url := fmt.Sprintf("%s/accounts/%s/workers/scripts/%s/secrets", cfAPIBase, accountID, workerName)
	_, err := cfDo("PUT", url, token, bytes.NewReader(payload), "application/json")
	if err != nil {
		return fmt.Errorf("upsert secret %s: %w", secretName, err)
	}
	return nil
}

// VerifyGitHubPAT checks that pat has push access to owner/repo.
func VerifyGitHubPAT(pat, owner, repo string) error {
	url := fmt.Sprintf("%s/repos/%s/%s", ghAPIBase, owner, repo)
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+pat)
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")

	resp, err := httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("GitHub API: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
		return fmt.Errorf("GitHub PAT rejected (HTTP %d): check token scopes (needs repo)", resp.StatusCode)
	}
	if resp.StatusCode == http.StatusNotFound {
		return fmt.Errorf("repo %s/%s not found or PAT lacks access", owner, repo)
	}
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("GitHub API returned HTTP %d", resp.StatusCode)
	}
	var repoInfo struct {
		Permissions struct {
			Push bool `json:"push"`
		} `json:"permissions"`
	}
	raw, _ := io.ReadAll(resp.Body)
	if err := json.Unmarshal(raw, &repoInfo); err != nil {
		return fmt.Errorf("GitHub API parse: %w", err)
	}
	if !repoInfo.Permissions.Push {
		return fmt.Errorf("PAT does not have push permission on %s/%s", owner, repo)
	}
	return nil
}

// DeleteWorker deletes a deployed CF Worker script. Best-effort: a 404 (worker
// not found) is treated as success so callers can safely call this even if the
// deploy partially failed.
func DeleteWorker(accountID, token, workerName string) error {
	url := fmt.Sprintf("%s/accounts/%s/workers/scripts/%s", cfAPIBase, accountID, workerName)
	req, err := http.NewRequest("DELETE", url, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("CF delete worker: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotFound {
		return nil // already gone
	}
	raw, _ := io.ReadAll(resp.Body)
	var env cfResponse
	if err := json.Unmarshal(raw, &env); err != nil {
		return fmt.Errorf("CF delete worker parse (%d): %w", resp.StatusCode, err)
	}
	if !env.Success {
		if len(env.Errors) > 0 {
			return env.Errors[0]
		}
		return fmt.Errorf("CF delete worker error (HTTP %d)", resp.StatusCode)
	}
	return nil
}

// sanitizeID converts a workspace ID to a CF-safe script name component
// (lowercase alphanumeric + hyphens, max 30 chars).
func sanitizeID(id string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(id) {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '-' {
			b.WriteRune(r)
		} else {
			b.WriteRune('-')
		}
		if b.Len() >= 30 {
			break
		}
	}
	return strings.Trim(b.String(), "-")
}
