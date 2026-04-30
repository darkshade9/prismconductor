package llm

import (
	"context"
	"strings"
	"testing"

	"prismconductor/internal/types"
)

func TestRegistryGetAndOrder(t *testing.T) {
	r := NewRegistry(NewClaudeProvider(), NewOpenAIProvider())
	if _, ok := r.Get(types.ProviderClaude); !ok {
		t.Fatal("expected claude provider in registry")
	}
	if _, ok := r.Get(types.Provider("missing")); ok {
		t.Fatal("did not expect provider 'missing'")
	}
	all := r.All()
	if len(all) != 2 {
		t.Fatalf("All returned %d providers, want 2", len(all))
	}
	if all[0].Kind() != types.ProviderClaude {
		t.Fatalf("All[0] = %s, want claude (registration order)", all[0].Kind())
	}
}

func TestRegistryCanSpawn(t *testing.T) {
	r := NewRegistry(NewClaudeProvider(), NewOpenAIProvider(), NewLiteLLMProvider(), NewLMStudioProvider(), NewOllamaProvider())
	if !r.CanSpawn(types.ProviderClaude) {
		t.Fatal("claude.CanSpawn should be true")
	}
	// Issue #58: harness-v1 flips CanSpawn() to true on the four
	// OpenAI-compat providers. The session manager dispatches them through
	// the in-process loop instead of pty.Start.
	for _, kind := range []types.Provider{
		types.ProviderOpenAI,
		types.ProviderLiteLLM,
		types.ProviderLMStudio,
		types.ProviderOllama,
	} {
		if !r.CanSpawn(kind) {
			t.Errorf("%s.CanSpawn should be true post harness-v1", kind)
		}
	}
	if r.CanSpawn(types.Provider("missing")) {
		t.Fatal("unknown provider's CanSpawn should be false")
	}
}

func TestClaudeSpawnArgsThreadsModel(t *testing.T) {
	p := NewClaudeProvider()
	args, err := p.SpawnArgs(types.Pool{Model: "claude-sonnet-4-6"}, "do the thing")
	if err != nil {
		t.Fatalf("SpawnArgs error: %v", err)
	}
	joined := strings.Join(args, " ")
	if !strings.Contains(joined, "--model claude-sonnet-4-6") {
		t.Errorf("argv missing --model claude-sonnet-4-6: %v", args)
	}
	if args[len(args)-1] != "do the thing" {
		t.Errorf("last arg = %q, want prompt", args[len(args)-1])
	}
}

func TestClaudeSpawnArgsOmitsEmptyModel(t *testing.T) {
	args, err := NewClaudeProvider().SpawnArgs(types.Pool{}, "p")
	if err != nil {
		t.Fatalf("SpawnArgs error: %v", err)
	}
	for _, a := range args {
		if a == "--model" {
			t.Fatalf("--model present despite empty Pool.Model: %v", args)
		}
	}
}

func TestNonClaudeProvidersReturnNotSupportedForSpawnArgs(t *testing.T) {
	cases := []Provider{
		NewOpenAIProvider(),
		NewLiteLLMProvider(),
		NewLMStudioProvider(),
		NewOllamaProvider(),
	}
	for _, p := range cases {
		t.Run(string(p.Kind()), func(t *testing.T) {
			// Issue #58: CanSpawn flipped to true; the session manager now
			// drives these via the harness when SpawnArgs returns
			// ErrNotSupported. SpawnArgs itself still signals "harness me"
			// with ErrNotSupported.
			if !p.CanSpawn() {
				t.Fatalf("%s.CanSpawn should be true post harness-v1", p.Kind())
			}
			if _, err := p.SpawnArgs(types.Pool{}, "x"); err != ErrNotSupported {
				t.Fatalf("%s.SpawnArgs returned %v, want ErrNotSupported", p.Kind(), err)
			}
		})
	}
}

// TestClaudeToolChatNotSupported pins Claude's harness-side behavior: until
// the harness drives Claude pools too (separate ticket), Claude's ToolChat
// returns ErrNotSupported and the session manager keeps the PTY/subprocess
// strategy via SpawnArgs.
func TestClaudeToolChatNotSupported(t *testing.T) {
	p := NewClaudeProvider()
	if _, err := p.ToolChat(context.Background(), types.Pool{}, ChatRequest{}); err != ErrNotSupported {
		t.Fatalf("claude.ToolChat returned %v, want ErrNotSupported", err)
	}
}

// TestProvidersImplementToolChat is a compile-time-style guard that every
// registered provider satisfies the harness-v1 Provider interface (issue
// #58). If a future provider is added without ToolChat, this test fails to
// compile.
func TestProvidersImplementToolChat(t *testing.T) {
	cases := []Provider{
		NewClaudeProvider(),
		NewOpenAIProvider(),
		NewLiteLLMProvider(),
		NewLMStudioProvider(),
		NewOllamaProvider(),
	}
	for _, p := range cases {
		var _ Provider = p
		_ = p.Kind()
	}
}

func TestDecodeOpenAIModelsParsesEnvelope(t *testing.T) {
	body := `{"data":[{"id":"gpt-4o-mini"},{"id":"gpt-4o"}]}`
	got, err := decodeOpenAIModels(strings.NewReader(body))
	if err != nil {
		t.Fatalf("decodeOpenAIModels error: %v", err)
	}
	want := []string{"gpt-4o", "gpt-4o-mini"}
	if len(got) != len(want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	for i := range got {
		if got[i] != want[i] {
			t.Fatalf("got[%d] = %s, want %s (sorted)", i, got[i], want[i])
		}
	}
}

// TestDecodeOpenAIModelsStripsGeminiPrefix pins the Gemini OpenAI-compat
// dropdown fix: /v1beta/openai/models returns IDs like `models/gemini-2.0-flash`
// but the chat-completions surface expects the bare ID. The decoder strips the
// `models/` prefix so the UI surfaces a model name the user can pick directly.
func TestDecodeOpenAIModelsStripsGeminiPrefix(t *testing.T) {
	body := `{"data":[{"id":"models/gemini-2.0-flash"},{"id":"models/gemini-2.5-flash"},{"id":"gpt-4o"}]}`
	got, err := decodeOpenAIModels(strings.NewReader(body))
	if err != nil {
		t.Fatalf("decodeOpenAIModels error: %v", err)
	}
	want := []string{"gemini-2.0-flash", "gemini-2.5-flash", "gpt-4o"}
	if len(got) != len(want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	for i := range got {
		if got[i] != want[i] {
			t.Fatalf("got[%d] = %s, want %s", i, got[i], want[i])
		}
	}
}

func TestClaudeListModelsReturnsCanonicalSet(t *testing.T) {
	got, err := NewClaudeProvider().ListModels(context.Background(), types.Pool{})
	if err != nil {
		t.Fatalf("ListModels error: %v", err)
	}
	if len(got) == 0 {
		t.Fatal("expected at least one Claude model in canonical set")
	}
}
