// Package ollama is a thin HTTP client for the local LLM orchestrator (PRISMCONDUCTOR_PLAN.md §11).
package ollama

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

const DefaultURL = "http://localhost:11434"
const DefaultModel = "qwen2.5:14b-instruct"

type Client struct {
	URL   string
	Model string
	HTTP  *http.Client
}

func New(url, model string) *Client {
	if url == "" {
		url = DefaultURL
	}
	if model == "" {
		model = DefaultModel
	}
	return &Client{URL: url, Model: model, HTTP: &http.Client{Timeout: 5 * time.Minute}}
}

// Available returns true iff the configured model is present in `ollama list`.
func (c *Client) Available(ctx context.Context) (bool, error) {
	req, _ := http.NewRequestWithContext(ctx, "GET", c.URL+"/api/tags", nil)
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return false, err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	var out struct {
		Models []struct{ Name string } `json:"models"`
	}
	if err := json.Unmarshal(body, &out); err != nil {
		return false, err
	}
	for _, m := range out.Models {
		if m.Name == c.Model {
			return true, nil
		}
	}
	return false, nil
}

type GenerateRequest struct {
	Model       string         `json:"model"`
	Prompt      string         `json:"prompt"`
	System      string         `json:"system,omitempty"`
	Stream      bool           `json:"stream"`
	Format      string         `json:"format,omitempty"` // "json"
	Options     map[string]any `json:"options,omitempty"`
}

type GenerateResponse struct {
	Response string `json:"response"`
	Done     bool   `json:"done"`
}

func (c *Client) Generate(ctx context.Context, system, prompt string) (string, error) {
	body, _ := json.Marshal(GenerateRequest{
		Model:   c.Model,
		Prompt:  prompt,
		System:  system,
		Stream:  false,
		Format:  "json",
		Options: map[string]any{"temperature": 0.0},
	})
	req, _ := http.NewRequestWithContext(ctx, "POST", c.URL+"/api/generate", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		raw, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("ollama %d: %s", resp.StatusCode, string(raw))
	}
	var out GenerateResponse
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return "", err
	}
	return out.Response, nil
}
