package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"time"

	"prismconductor/internal/types"
)

const (
	defaultAnthropicMessagesEndpoint = "https://api.anthropic.com/v1/messages"
	anthropicAPIVersion              = "2023-06-01"
)

type claudeProvider struct {
	client *http.Client
}

func NewClaudeProvider() Provider {
	return claudeProvider{client: &http.Client{Timeout: 5 * time.Minute}}
}

func (claudeProvider) Kind() types.Provider    { return types.ProviderClaude }
func (claudeProvider) DisplayName() string     { return "Claude" }
func (claudeProvider) DefaultEndpoint() string { return "" }
func (claudeProvider) NeedsAPIKey() bool       { return false }
func (claudeProvider) CanSpawn() bool          { return true }

// Claude has no listing API; CLAUDE.md "Claude 4.X" family is the canonical
// set. The CLI accepts arbitrary --model IDs so the user can still type a
// preview ID into the modal.
func (claudeProvider) ListModels(_ context.Context, _ types.Pool) ([]string, error) {
	return []string{
		"claude-opus-4-7",
		"claude-sonnet-4-6",
		"claude-haiku-4-5-20251001",
	}, nil
}

// SpawnArgs mirrors the original session/manager.go:claudeArgs helper, threading
// pool.Model through --model when set.
func (claudeProvider) SpawnArgs(p types.Pool, prompt string) ([]string, error) {
	args := []string{
		"claude",
		"-p",
		"--output-format", "stream-json",
		"--include-partial-messages",
		"--verbose",
		"--permission-mode", "bypassPermissions",
	}
	if p.Model != "" {
		args = append(args, "--model", p.Model)
	}
	return append(args, prompt), nil
}

// ChatJSON sends a system+user prompt to the Anthropic Messages API and
// returns the assistant's content. When no API key is configured (neither
// p.APIKey nor ANTHROPIC_API_KEY), falls back to `claude -p --output-format
// json` so the orchestrator works on a developer machine that only has the
// Claude CLI authenticated.
func (c claudeProvider) ChatJSON(ctx context.Context, p types.Pool, system, user string) (string, error) {
	model := p.Model
	if model == "" {
		model = "claude-haiku-4-5-20251001"
	}
	key := p.APIKey
	if key == "" {
		key = os.Getenv("ANTHROPIC_API_KEY")
	}
	if key == "" {
		return claudeCLIFallback(ctx, model, system, user)
	}
	endpoint := strings.TrimRight(p.Endpoint, "/")
	if endpoint == "" {
		endpoint = defaultAnthropicMessagesEndpoint
	} else if !strings.HasSuffix(endpoint, "/v1/messages") {
		endpoint = endpoint + "/v1/messages"
	}
	reqBody := map[string]any{
		"model":      model,
		"system":     system,
		"max_tokens": 4096,
		"messages": []map[string]string{
			{"role": "user", "content": user},
		},
	}
	body, _ := json.Marshal(reqBody)
	req, err := http.NewRequestWithContext(ctx, "POST", endpoint, bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("x-api-key", key)
	req.Header.Set("anthropic-version", anthropicAPIVersion)
	resp, err := c.client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", fmt.Errorf("anthropic HTTP %d: %s", resp.StatusCode, snippetN(string(raw), 200))
	}
	var out struct {
		Content []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"content"`
		Error *struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		return "", fmt.Errorf("decode response: %w; body: %s", err, snippetN(string(raw), 200))
	}
	if out.Error != nil {
		return "", fmt.Errorf("anthropic error: %s", out.Error.Message)
	}
	for _, b := range out.Content {
		if b.Type == "text" && b.Text != "" {
			return b.Text, nil
		}
	}
	return "", fmt.Errorf("anthropic returned no text content; body: %s", snippetN(string(raw), 200))
}

// claudeCLIFallback shells `claude -p --output-format json` with a combined
// prompt. Requires the user to be logged in via `claude` CLI. The CLI doesn't
// expose a system-prompt flag for the headless print mode, so system+user are
// concatenated; the orchestrator's prompt template tolerates either shape.
func claudeCLIFallback(ctx context.Context, model, system, user string) (string, error) {
	prompt := strings.TrimSpace(system) + "\n\n" + strings.TrimSpace(user)
	args := []string{"-p", "--output-format", "json"}
	if model != "" {
		args = append(args, "--model", model)
	}
	args = append(args, prompt)
	cmd := exec.CommandContext(ctx, "claude", args...)
	stdout, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("claude CLI fallback: %w", err)
	}
	var out struct {
		Result string `json:"result"`
	}
	if err := json.Unmarshal(stdout, &out); err != nil {
		return string(stdout), nil
	}
	return out.Result, nil
}
