// Package ollama is the local-LLM HTTP client.
//
// Despite the package name, it speaks the OpenAI-compatible chat-completions
// API — both Ollama (`OPENAI compat at /v1/chat/completions`) and LM Studio
// expose it, so a single client works against either. The endpoint URL is
// stored as the *base* (e.g. http://localhost:11434), and we append the right
// path internally.
package ollama

import (
	"bytes"
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
	return &Client{URL: strings.TrimRight(url, "/"), Model: model, HTTP: &http.Client{Timeout: 5 * time.Minute}}
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

type chatRequest struct {
	Model       string        `json:"model"`
	Messages    []chatMessage `json:"messages"`
	Temperature float64       `json:"temperature"`
	Stream      bool          `json:"stream"`
}

type chatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type chatResponse struct {
	Choices []struct {
		Message chatMessage `json:"message"`
	} `json:"choices"`
	Error *struct {
		Message string `json:"message"`
	} `json:"error"`
}

// Generate sends a system + user prompt and returns the model's content.
// Caller is responsible for parsing JSON from the content.
func (c *Client) Generate(ctx context.Context, system, prompt string) (string, error) {
	body, _ := json.Marshal(chatRequest{
		Model:       c.Model,
		Temperature: 0.0,
		Stream:      false,
		Messages: []chatMessage{
			{Role: "system", Content: system},
			{Role: "user", Content: prompt},
		},
	})
	req, _ := http.NewRequestWithContext(ctx, "POST", c.URL+"/v1/chat/completions", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != 200 {
		return "", fmt.Errorf("LLM HTTP %d: %s", resp.StatusCode, string(raw))
	}
	var out chatResponse
	if err := json.Unmarshal(raw, &out); err != nil {
		return "", fmt.Errorf("decode response: %w; body: %s", err, snippet(string(raw), 200))
	}
	if out.Error != nil {
		return "", fmt.Errorf("LLM error: %s", out.Error.Message)
	}
	if len(out.Choices) == 0 {
		return "", fmt.Errorf("LLM returned no choices; body: %s", snippet(string(raw), 200))
	}
	return out.Choices[0].Message.Content, nil
}

func snippet(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}
