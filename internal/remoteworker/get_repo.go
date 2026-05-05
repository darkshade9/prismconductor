package remoteworker

import (
	"encoding/json"
	"fmt"
	"net/http"
)

// GetRepoDefaultBranch returns the default branch of owner/repo using the
// supplied GitHub PAT for authentication. Returns an error if the repo is
// not found (HTTP 404) or the PAT lacks Metadata read access (HTTP 401/403).
func GetRepoDefaultBranch(pat, owner, repo string) (string, error) {
	url := fmt.Sprintf("%s/repos/%s/%s", ghAPIBase, owner, repo)
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("Authorization", "Bearer "+pat)
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")

	resp, err := httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("GitHub API request: %w", err)
	}
	defer resp.Body.Close()

	switch resp.StatusCode {
	case http.StatusNotFound:
		return "", fmt.Errorf("repository %s/%s not found (check URL and PAT access)", owner, repo)
	case http.StatusUnauthorized, http.StatusForbidden:
		return "", fmt.Errorf("GitHub PAT does not have Metadata access to %s/%s", owner, repo)
	case http.StatusOK:
		// handled below
	default:
		return "", fmt.Errorf("GitHub API returned HTTP %d for %s/%s", resp.StatusCode, owner, repo)
	}

	var result struct {
		DefaultBranch string `json:"default_branch"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", fmt.Errorf("parse GitHub API response: %w", err)
	}
	if result.DefaultBranch == "" {
		return "", fmt.Errorf("GitHub API returned empty default_branch for %s/%s", owner, repo)
	}
	return result.DefaultBranch, nil
}
