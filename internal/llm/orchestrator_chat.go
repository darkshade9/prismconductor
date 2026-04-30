package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strings"
)

// versionedPathRE matches a baseURL whose path already ends with a
// version-like segment (`/v1`, `/v2beta`, `/v1beta/openai`, etc.). When it
// matches we append only `/chat/completions`; otherwise we prepend `/v1/`.
//
// Concretely: Gemini's OpenAI-compat base is
// `https://generativelanguage.googleapis.com/v1beta/openai`, OpenAI's default
// is `https://api.openai.com/v1`, and LM Studio / Ollama / LiteLLM defaults
// are bare `http://host:port`. All three shapes need to resolve to a single
// `…/chat/completions` URL.
var versionedPathRE = regexp.MustCompile(`/v\d+[A-Za-z0-9]*(/[A-Za-z0-9]+)*/?$`)

// chatCompletionsURL composes the final POST URL for an OpenAI-compatible
// chat-completions call, idempotent on the trailing version segment.
func chatCompletionsURL(baseURL string) string {
	return openAICompatPath(baseURL, "chat/completions")
}

// modelsURL composes the GET URL for an OpenAI-compatible /models listing.
func modelsURL(baseURL string) string {
	return openAICompatPath(baseURL, "models")
}

func openAICompatPath(baseURL, leaf string) string {
	trimmed := strings.TrimRight(baseURL, "/")
	if versionedPathRE.MatchString(trimmed) {
		return trimmed + "/" + leaf
	}
	return trimmed + "/v1/" + leaf
}

// openAICompatChat sends a system+user prompt to an OpenAI-compatible
// /v1/chat/completions endpoint and returns the assistant's content. Shared by
// every provider whose orchestrator path goes through OpenAI-compat (OpenAI,
// LM Studio, LiteLLM, Ollama).
func openAICompatChat(ctx context.Context, client *http.Client, baseURL, apiKey, model, system, user string) (string, error) {
	if model == "" {
		return "", fmt.Errorf("orchestrator chat: model required")
	}
	url := chatCompletionsURL(baseURL)
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
	if isPaddingOnly(raw) {
		return "", fmt.Errorf("LLM returned only keep-alive padding (request likely timed out before model finished). Try a smaller/faster model. body: %s", snippetN(string(raw), 200))
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

// isPaddingOnly returns true when the response body contains nothing but
// whitespace and/or SSE-comment lines (`: keepalive\n`). OpenRouter and
// similar gateways emit these while a slow upstream call is processing; if
// the client timeout fires before the actual JSON body arrives we'd
// otherwise crash the decoder with an unhelpful "unexpected end of JSON
// input" error.
func isPaddingOnly(b []byte) bool {
	for _, line := range strings.Split(string(b), "\n") {
		t := strings.TrimSpace(line)
		if t == "" {
			continue
		}
		if strings.HasPrefix(t, ":") {
			// SSE comment. Keep scanning.
			continue
		}
		// Real content found.
		return false
	}
	return true
}

// openAIToolChat is the harness-side sibling of openAICompatChat. It serializes
// `tools[]` and `tool_choice:"auto"` and decodes `choices[0].message.tool_calls`
// into the harness ChatResponse shape (issue #58).
//
// Non-streaming for v1 — buffers the full response and returns one
// ChatResponse. Shared by every OpenAI-compat provider (OpenAI, Ollama, LM
// Studio, LiteLLM); each provider's ToolChat method resolves endpoint + auth
// then delegates here.
func openAIToolChat(ctx context.Context, client *http.Client, baseURL, apiKey, model string, req ChatRequest) (ChatResponse, error) {
	if model == "" {
		return ChatResponse{}, fmt.Errorf("tool chat: model required")
	}
	url := chatCompletionsURL(baseURL)

	msgs := make([]map[string]any, 0, len(req.History)+1)
	if req.System != "" {
		msgs = append(msgs, map[string]any{
			"role":    "system",
			"content": req.System,
		})
	}
	for _, m := range req.History {
		switch m.Role {
		case "tool":
			msgs = append(msgs, map[string]any{
				"role":         "tool",
				"content":      m.Content,
				"tool_call_id": m.ToolCallID,
			})
		case "assistant":
			am := map[string]any{
				"role":    "assistant",
				"content": m.Content,
			}
			if len(m.ToolCalls) > 0 {
				calls := make([]map[string]any, len(m.ToolCalls))
				for i, c := range m.ToolCalls {
					args := string(c.Args)
					if args == "" {
						args = "{}"
					}
					calls[i] = map[string]any{
						"id":   c.ID,
						"type": "function",
						"function": map[string]any{
							"name":      c.Name,
							"arguments": args,
						},
					}
				}
				am["tool_calls"] = calls
			}
			msgs = append(msgs, am)
		default:
			msgs = append(msgs, map[string]any{
				"role":    m.Role,
				"content": m.Content,
			})
		}
	}

	body := map[string]any{
		"model":       model,
		"temperature": 0.0,
		"stream":      false,
		"messages":    msgs,
	}
	if len(req.Tools) > 0 {
		tools := make([]map[string]any, len(req.Tools))
		for i, t := range req.Tools {
			var params any
			if len(t.Parameters) > 0 {
				params = json.RawMessage(t.Parameters)
			} else {
				params = map[string]any{"type": "object"}
			}
			tools[i] = map[string]any{
				"type": "function",
				"function": map[string]any{
					"name":        t.Name,
					"description": t.Description,
					"parameters":  params,
				},
			}
		}
		body["tools"] = tools
		body["tool_choice"] = "auto"
	}

	raw, _ := json.Marshal(body)
	httpReq, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(raw))
	if err != nil {
		return ChatResponse{}, err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	if apiKey != "" {
		httpReq.Header.Set("Authorization", "Bearer "+apiKey)
	}
	resp, err := client.Do(httpReq)
	if err != nil {
		return ChatResponse{}, err
	}
	defer resp.Body.Close()
	respRaw, _ := io.ReadAll(resp.Body)
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return ChatResponse{}, fmt.Errorf("LLM HTTP %d: %s", resp.StatusCode, snippetN(string(respRaw), 200))
	}
	// OpenRouter (and similar gateways) send whitespace + SSE-comment
	// keep-alive padding while a slow request is processing. If the client
	// timeout fires before the actual JSON body arrives we end up with a
	// padding-only body that would crash the decoder with a useless
	// "unexpected end of JSON input". Detect and surface the real cause.
	if isPaddingOnly(respRaw) {
		return ChatResponse{}, fmt.Errorf("LLM returned only keep-alive padding (request likely timed out before model finished). Try a smaller/faster model or a shorter context. body: %s", snippetN(string(respRaw), 200))
	}
	var out struct {
		Choices []struct {
			FinishReason string `json:"finish_reason"`
			Message      struct {
				Content   string `json:"content"`
				ToolCalls []struct {
					ID       string `json:"id"`
					Type     string `json:"type"`
					Function struct {
						Name      string `json:"name"`
						Arguments string `json:"arguments"`
					} `json:"function"`
				} `json:"tool_calls"`
			} `json:"message"`
		} `json:"choices"`
		Usage struct {
			PromptTokens     int `json:"prompt_tokens"`
			CompletionTokens int `json:"completion_tokens"`
		} `json:"usage"`
		Error *struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal(respRaw, &out); err != nil {
		return ChatResponse{}, fmt.Errorf("decode response: %w; body: %s", err, snippetN(string(respRaw), 200))
	}
	if out.Error != nil {
		return ChatResponse{}, fmt.Errorf("LLM error: %s", out.Error.Message)
	}
	if len(out.Choices) == 0 {
		return ChatResponse{}, fmt.Errorf("LLM returned no choices; body: %s", snippetN(string(respRaw), 200))
	}
	choice := out.Choices[0]
	calls := make([]ToolCall, 0, len(choice.Message.ToolCalls))
	for _, c := range choice.Message.ToolCalls {
		calls = append(calls, ToolCall{
			ID:   c.ID,
			Name: c.Function.Name,
			Args: json.RawMessage(c.Function.Arguments),
		})
	}
	return ChatResponse{
		Content:   choice.Message.Content,
		ToolCalls: calls,
		Stop:      len(calls) == 0,
		Usage: UsageCounts{
			InputTokens:  out.Usage.PromptTokens,
			OutputTokens: out.Usage.CompletionTokens,
		},
	}, nil
}
