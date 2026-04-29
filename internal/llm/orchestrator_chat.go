package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
)

// openAICompatChat sends a system+user prompt to an OpenAI-compatible
// /v1/chat/completions endpoint and returns the assistant's content. Shared by
// every provider whose orchestrator path goes through OpenAI-compat (OpenAI,
// LM Studio, LiteLLM, Ollama).
func openAICompatChat(ctx context.Context, client *http.Client, baseURL, apiKey, model, system, user string) (string, error) {
	if model == "" {
		return "", fmt.Errorf("orchestrator chat: model required")
	}
	url := strings.TrimRight(baseURL, "/") + "/v1/chat/completions"
	reqBody := map[string]any{
		"model":       model,
		"temperature": 0.0,
		"stream":      false,
		"messages": []map[string]string{
			{"role": "system", "content": system},
			{"role": "user", "content": user},
		},
	}
	body, _ := json.Marshal(reqBody)
	req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")
	if apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+apiKey)
	}
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", fmt.Errorf("LLM HTTP %d: %s", resp.StatusCode, snippetN(string(raw), 200))
	}
	var out struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
		Error *struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		return "", fmt.Errorf("decode response: %w; body: %s", err, snippetN(string(raw), 200))
	}
	if out.Error != nil {
		return "", fmt.Errorf("LLM error: %s", out.Error.Message)
	}
	if len(out.Choices) == 0 {
		return "", fmt.Errorf("LLM returned no choices; body: %s", snippetN(string(raw), 200))
	}
	return out.Choices[0].Message.Content, nil
}

func snippetN(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}
