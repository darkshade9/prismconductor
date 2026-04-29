// Package ollama is a thin adapter that probes an OpenAI-compatible local LLM
// endpoint for liveness. Despite the package name, it accepts any base URL
// that exposes either /api/tags or /v1/models — covers Ollama, LM Studio, and
// any other server speaking the same shape.
//
// Issue #39 moved the orchestrator's chat path to internal/llm.Provider's
// ChatJSON method. This package now only exists for the (legacy, may be
// dropped) UI presence probe.
package ollama

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

const (
	DefaultURL   = "http://localhost:11434"
	DefaultModel = "qwen2.5:14b-instruct"
)

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
	return &Client{URL: strings.TrimRight(url, "/"), Model: model, HTTP: &http.Client{Timeout: 30 * time.Second}}
}

// Available probes whichever models endpoint the server speaks (Ollama
// `/api/tags` first, then OpenAI-compat `/v1/models`) and reports whether the
// configured model name is present.
func (c *Client) Available(ctx context.Context) (bool, error) {
	if ok, err := c.checkOllamaTags(ctx); err == nil && ok {
		return true, nil
	}
	return c.checkOpenAIModels(ctx)
}

func (c *Client) checkOllamaTags(ctx context.Context) (bool, error) {
	req, _ := http.NewRequestWithContext(ctx, "GET", c.URL+"/api/tags", nil)
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return false, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return false, fmt.Errorf("status %d", resp.StatusCode)
	}
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

func (c *Client) checkOpenAIModels(ctx context.Context) (bool, error) {
	req, _ := http.NewRequestWithContext(ctx, "GET", c.URL+"/v1/models", nil)
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return false, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return false, fmt.Errorf("status %d", resp.StatusCode)
	}
	var out struct {
		Data []struct{ ID string } `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return false, err
	}
	for _, m := range out.Data {
		if m.ID == c.Model {
			return true, nil
		}
	}
	return false, nil
}
