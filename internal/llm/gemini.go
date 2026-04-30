package llm

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"

	anyllm "github.com/mozilla-ai/any-llm-go"
	anyllmproviders "github.com/mozilla-ai/any-llm-go/providers"
	"github.com/mozilla-ai/any-llm-go/providers/gemini"

	"prismconductor/internal/types"
)

// geminiProvider is the first any-llm-go-backed adapter (issue #68 / spike).
// It satisfies our Provider interface by translating ChatRequest <-> any-llm-go's
// CompletionParams and delegating to the gemini package's native Google API
// client. Two reasons we don't route Gemini through ProviderOpenAI's
// /v1beta/openai shim:
//
//  1. The shim doesn't round-trip `thought_signature`, which Gemini 2.5-class
//     thinking models require on every tool turn. That gap killed every
//     previous Gemini plan run on tool-using turns.
//  2. The shim returns model IDs as `models/<id>`, which the chat-completions
//     surface then rejects. The native client uses bare IDs uniformly.
//
// If this trial holds up, the four OpenAI-compat providers (openai, ollama,
// lmstudio, litellm) get the same any-llm-go treatment in a follow-up.
type geminiProvider struct{}

func NewGeminiProvider() Provider { return geminiProvider{} }

func (geminiProvider) Kind() types.Provider    { return types.ProviderGemini }
func (geminiProvider) DisplayName() string     { return "Gemini" }
func (geminiProvider) DefaultEndpoint() string { return "" }
func (geminiProvider) NeedsAPIKey() bool       { return true }
func (geminiProvider) CanSpawn() bool          { return true }

// SpawnArgs returns ErrNotSupported so the session manager dispatches Gemini
// pools through the harness loop. The harness then calls ToolChat below.
func (geminiProvider) SpawnArgs(_ types.Pool, _ string) ([]string, error) {
	return nil, ErrNotSupported
}

// newClient constructs a fresh any-llm-go gemini provider scoped to a single
// pool's API key. The library reads `GEMINI_API_KEY` / `GOOGLE_API_KEY` from
// the env when no key is provided, so unconfigured pools still work via env.
func (geminiProvider) newClient(p types.Pool) (*gemini.Provider, error) {
	opts := []anyllm.Option{}
	key := strings.TrimSpace(p.APIKey)
	if key == "" {
		// Match openai.go behavior: fall back to an env var so an operator
		// running a single key can leave the pool's APIKey field empty.
		if env := os.Getenv("GEMINI_API_KEY"); env != "" {
			key = env
		} else if env := os.Getenv("GOOGLE_API_KEY"); env != "" {
			key = env
		}
	}
	if key != "" {
		opts = append(opts, anyllm.WithAPIKey(key))
	}
	return gemini.New(opts...)
}

func (g geminiProvider) ListModels(ctx context.Context, p types.Pool) ([]string, error) {
	c, err := g.newClient(p)
	if err != nil {
		return nil, err
	}
	resp, err := c.ListModels(ctx)
	if err != nil {
		return nil, err
	}
	ids := make([]string, 0, len(resp.Data))
	for _, m := range resp.Data {
		// Gemini's native API also returns `models/<id>`; strip for parity
		// with the OpenAI-compat path so the dropdown surfaces a usable name.
		id := strings.TrimPrefix(m.ID, "models/")
		if id != "" {
			ids = append(ids, id)
		}
	}
	return ids, nil
}

// ChatJSON drives a single non-tool prompt for the orchestrator's rank+deps
// call. The harness path (ToolChat) is the load-bearing one for plan/work.
func (g geminiProvider) ChatJSON(ctx context.Context, p types.Pool, system, user string) (string, error) {
	c, err := g.newClient(p)
	if err != nil {
		return "", err
	}
	if p.Model == "" {
		return "", fmt.Errorf("gemini: model required on pool %q", p.ID)
	}
	resp, err := c.Completion(ctx, anyllm.CompletionParams{
		Model: p.Model,
		Messages: []anyllm.Message{
			{Role: anyllm.RoleSystem, Content: system},
			{Role: anyllm.RoleUser, Content: user},
		},
	})
	if err != nil {
		return "", err
	}
	if len(resp.Choices) == 0 {
		return "", fmt.Errorf("gemini: empty choices")
	}
	return contentToString(resp.Choices[0].Message.Content), nil
}

// ToolChat is the harness's per-turn callout. We translate ChatRequest into
// any-llm-go's CompletionParams (preserving any prior tool-call ↔ tool-result
// pairs so `thought_signature` round-trips internally), call Completion, then
// translate the response back. Reasoning content is folded into the visible
// Content so the §10.3 sentinels (Plan written / BLOCKED / etc.) still match
// even when the model emits them as part of its thinking trace.
func (g geminiProvider) ToolChat(ctx context.Context, p types.Pool, req ChatRequest) (ChatResponse, error) {
	c, err := g.newClient(p)
	if err != nil {
		return ChatResponse{}, err
	}
	if p.Model == "" {
		return ChatResponse{}, fmt.Errorf("gemini: model required on pool %q", p.ID)
	}

	msgs := make([]anyllm.Message, 0, len(req.History)+1)
	if req.System != "" {
		msgs = append(msgs, anyllm.Message{Role: anyllm.RoleSystem, Content: req.System})
	}
	for _, m := range req.History {
		am := anyllm.Message{
			Role:       m.Role,
			Content:    m.Content,
			ToolCallID: m.ToolCallID,
		}
		if len(m.ToolCalls) > 0 {
			am.ToolCalls = make([]anyllm.ToolCall, len(m.ToolCalls))
			for i, tc := range m.ToolCalls {
				args := string(tc.Args)
				if args == "" {
					args = "{}"
				}
				out := anyllm.ToolCall{
					ID:   tc.ID,
					Type: "function",
					Function: anyllm.FunctionCall{
						Name:      tc.Name,
						Arguments: args,
					},
				}
				// Round-trip the per-provider Extra blob so Gemini's
				// thought_signature survives multi-turn conversations.
				// `Extra` is keyed by provider name in any-llm-go; we
				// pass through verbatim.
				if len(tc.Extra) > 0 {
					out.Extra = map[string]anyllmproviders.ProviderData{}
					for k, v := range tc.Extra {
						if pd, ok := v.(map[string]any); ok {
							out.Extra[k] = anyllmproviders.ProviderData(pd)
						}
					}
				}
				am.ToolCalls[i] = out
			}
		}
		msgs = append(msgs, am)
	}

	tools := make([]anyllm.Tool, 0, len(req.Tools))
	for _, t := range req.Tools {
		var params map[string]any
		if len(t.Parameters) > 0 {
			if err := json.Unmarshal(t.Parameters, &params); err != nil {
				return ChatResponse{}, fmt.Errorf("gemini: tool %q parameters not valid JSON object: %w", t.Name, err)
			}
		} else {
			params = map[string]any{"type": "object"}
		}
		tools = append(tools, anyllm.Tool{
			Type: "function",
			Function: anyllm.Function{
				Name:        t.Name,
				Description: t.Description,
				Parameters:  params,
			},
		})
	}

	resp, err := c.Completion(ctx, anyllm.CompletionParams{
		Model:    p.Model,
		Messages: msgs,
		Tools:    tools,
		// ReasoningEffortAuto lets the model decide whether to think; the
		// returned Reasoning content is folded into ChatResponse.Content
		// below so sentinel detection still works.
		ReasoningEffort: anyllm.ReasoningEffortAuto,
	})
	if err != nil {
		return ChatResponse{}, err
	}
	if len(resp.Choices) == 0 {
		return ChatResponse{}, fmt.Errorf("gemini: empty choices")
	}
	choice := resp.Choices[0]

	calls := make([]ToolCall, 0, len(choice.Message.ToolCalls))
	for _, tc := range choice.Message.ToolCalls {
		call := ToolCall{
			ID:   tc.ID,
			Name: tc.Function.Name,
			Args: json.RawMessage(tc.Function.Arguments),
		}
		if len(tc.Extra) > 0 {
			call.Extra = make(map[string]any, len(tc.Extra))
			for k, pd := range tc.Extra {
				call.Extra[k] = map[string]any(pd)
			}
		}
		calls = append(calls, call)
	}

	content := contentToString(choice.Message.Content)
	if choice.Message.Reasoning != nil && choice.Message.Reasoning.Content != "" && content == "" {
		// Some thinking-tier models emit Plan-written-style sentinels in the
		// reasoning channel and leave Content empty. Surface the reasoning
		// text so matchPatterns can still see the sentinel.
		content = choice.Message.Reasoning.Content
	}

	out := ChatResponse{
		Content:   content,
		ToolCalls: calls,
		Stop:      len(calls) == 0,
	}
	if resp.Usage != nil {
		out.Usage = UsageCounts{
			InputTokens:  int(resp.Usage.PromptTokens),
			OutputTokens: int(resp.Usage.CompletionTokens),
		}
	}
	return out, nil
}

// contentToString unwraps any-llm-go's `Content any` field. The field can be
// either a plain string or a []ContentPart for multi-modal turns; we use
// plain strings everywhere and just take the first text part if a multipart
// response sneaks through.
func contentToString(v any) string {
	switch c := v.(type) {
	case string:
		return c
	case []anyllm.ContentPart:
		var b strings.Builder
		for _, part := range c {
			if part.Type == "text" {
				b.WriteString(part.Text)
			}
		}
		return b.String()
	default:
		return ""
	}
}
