// Package llm holds heterogeneous LLM provider drivers used by worker pools
// (PRISMCONDUCTOR_PLAN.md §6.6, issue #27).
//
// The Provider interface is the single seam through which the rest of the
// codebase reaches LLM-specific behavior (argv to spawn, models to list,
// whether spawning works today). Routing in workerpool/orchestrator stays
// provider-agnostic — those packages ask `CanSpawn()`, never compare against a
// specific Provider constant.
package llm

import (
	"context"
	"encoding/json"
	"errors"

	"prismconductor/internal/types"
)

// ErrNotSupported signals that the operation isn't wired up for a provider.
// Claude returns it from ToolChat (the harness only drives non-Claude pools
// today); the four OpenAI-compat providers return it from SpawnArgs because
// they have no native CLI to pty.Start — the harness drives their tool loop
// via ToolChat instead.
var ErrNotSupported = errors.New("llm: provider does not support this operation yet")

// Provider drives one heterogeneous worker fleet kind.
type Provider interface {
	Kind() types.Provider
	DisplayName() string
	DefaultEndpoint() string
	NeedsAPIKey() bool
	// APIKeyHelpURL returns a URL where the user can obtain an API key for this
	// provider, or "" if no help link is available. Used by the frontend's
	// ProviderEditModal to render a "Get your API key →" link.
	APIKeyHelpURL() string

	// CanSpawn reports whether the session manager can spawn a worker for
	// this provider — either via SpawnArgs (subprocess strategy) or via
	// ToolChat (harness strategy). The workerpool registry consults this to
	// decide eligibility, so the orchestrator never compares against
	// ProviderClaude literally.
	CanSpawn() bool

	// ListModels probes the endpoint for available model IDs. Returns
	// ErrNotSupported if the provider has no listing API; the UI then keeps
	// the model field as free-text.
	ListModels(ctx context.Context, p types.Pool) ([]string, error)

	// SpawnArgs returns the argv the session manager should pty.Start.
	// Returns ErrNotSupported for providers driven by the harness instead of
	// a subprocess. The session manager dispatches:
	//   - err == nil               → subprocess strategy (Claude today)
	//   - errors.Is(err, ErrNotSupported) → harness strategy (uses ToolChat)
	//   - other err                → surfaced to caller
	SpawnArgs(p types.Pool, prompt string) ([]string, error)

	// ChatJSON sends a system+user prompt and returns the raw model output.
	// Used by the orchestrator's rank+deps call (issue #39). Implementations
	// route through their native chat endpoint:
	//   - ProviderClaude:                  Anthropic /v1/messages, with a
	//                                      claude -p fallback if no API key.
	//   - ProviderOpenAI/LMStudio/LiteLLM: OpenAI-compat /v1/chat/completions.
	//   - ProviderOllama:                  /v1/chat/completions (ollama serves
	//                                      OpenAI-compat too).
	ChatJSON(ctx context.Context, p types.Pool, system, user string) (string, error)

	// ToolChat sends a tool-using chat turn and returns the assistant's
	// content + any requested tool calls (issue #58). Non-streaming for v1.
	// Claude returns ErrNotSupported — Claude pools keep the PTY/subprocess
	// path for now. The four OpenAI-compat providers route through their
	// /v1/chat/completions endpoint with `tools:` serialized.
	ToolChat(ctx context.Context, p types.Pool, req ChatRequest) (ChatResponse, error)
}

// ChatRequest is the wire-shape carried into Provider.ToolChat (issue #58).
type ChatRequest struct {
	System  string
	History []Message
	Tools   []ToolDef
}

// Message is one turn in the chat history. Role is "user", "assistant", or
// "tool". For assistant messages, ToolCalls may be populated. For tool
// messages, ToolCallID identifies which assistant tool call this result
// answers.
type Message struct {
	Role       string
	Content    string
	ToolCalls  []ToolCall
	ToolCallID string
}

// ToolDef advertises a tool the model can call. Parameters is JSONSchema.
type ToolDef struct {
	Name        string
	Description string
	Parameters  json.RawMessage
}

// ToolCall is one model-issued tool invocation. Args is JSON matching the
// corresponding ToolDef.Parameters.
//
// Extra is an opaque per-provider metadata blob round-tripped across turns.
// Gemini's `thought_signature` rides here on thinking-tier models; the
// harness must echo it back unchanged on the next turn or Gemini rejects the
// follow-up tool result. OpenAI-compat providers leave it nil.
type ToolCall struct {
	ID    string
	Name  string
	Args  json.RawMessage
	Extra map[string]any
}

// ChatResponse is the wire-shape returned from Provider.ToolChat.
// Stop is true when the model signals end-of-turn with no tool calls
// (i.e. native stop). Otherwise the harness sends each ToolCall through the
// tool registry and feeds the results back as additional Messages.
type ChatResponse struct {
	Content   string
	ToolCalls []ToolCall
	Stop      bool
	Usage     UsageCounts
}

// UsageCounts mirror the OpenAI usage block. Captured per-call but not yet
// aggregated into a cost dashboard (out of scope for #58).
type UsageCounts struct {
	InputTokens  int
	OutputTokens int
}

// Registry holds one Provider per kind plus a stable ordering for the UI.
type Registry struct {
	byKind map[types.Provider]Provider
	order  []types.Provider
}

func NewRegistry(ps ...Provider) *Registry {
	r := &Registry{byKind: make(map[types.Provider]Provider, len(ps))}
	for _, p := range ps {
		if _, dup := r.byKind[p.Kind()]; dup {
			continue
		}
		r.byKind[p.Kind()] = p
		r.order = append(r.order, p.Kind())
	}
	return r
}

func (r *Registry) Get(k types.Provider) (Provider, bool) {
	if r == nil {
		return nil, false
	}
	p, ok := r.byKind[k]
	return p, ok
}

func (r *Registry) All() []Provider {
	if r == nil {
		return nil
	}
	out := make([]Provider, 0, len(r.order))
	for _, k := range r.order {
		out = append(out, r.byKind[k])
	}
	return out
}

// CanSpawn is the workerpool.Registry's eligibility predicate.
func (r *Registry) CanSpawn(k types.Provider) bool {
	p, ok := r.Get(k)
	return ok && p.CanSpawn()
}
