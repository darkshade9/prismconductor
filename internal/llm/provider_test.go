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
	r := NewRegistry(NewClaudeProvider(), NewOpenAIProvider())
	if !r.CanSpawn(types.ProviderClaude) {
		t.Fatal("claude.CanSpawn should be true")
	}
	if r.CanSpawn(types.ProviderOpenAI) {
		t.Fatal("openai.CanSpawn should be false until harness-v1")
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
			if p.CanSpawn() {
				t.Fatalf("%s.CanSpawn should be false until harness-v1", p.Kind())
			}
			if _, err := p.SpawnArgs(types.Pool{}, "x"); err != ErrNotSupported {
				t.Fatalf("%s.SpawnArgs returned %v, want ErrNotSupported", p.Kind(), err)
			}
		})
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

func TestClaudeListModelsReturnsCanonicalSet(t *testing.T) {
	got, err := NewClaudeProvider().ListModels(context.Background(), types.Pool{})
	if err != nil {
		t.Fatalf("ListModels error: %v", err)
	}
	if len(got) == 0 {
		t.Fatal("expected at least one Claude model in canonical set")
	}
}
