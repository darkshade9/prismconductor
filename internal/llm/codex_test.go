package llm

import (
	"context"
	"strings"
	"testing"

	"prismconductor/internal/types"
)

func TestCodexProviderKind(t *testing.T) {
	p := NewCodexProvider()
	if p.Kind() != types.ProviderCodex {
		t.Fatalf("Kind() = %s, want %s", p.Kind(), types.ProviderCodex)
	}
}

func TestCodexProviderNeedsNoAPIKey(t *testing.T) {
	p := NewCodexProvider()
	if p.NeedsAPIKey() {
		t.Fatal("codex uses browser OAuth — NeedsAPIKey() should be false")
	}
}

func TestCodexProviderCanSpawn(t *testing.T) {
	p := NewCodexProvider()
	if !p.CanSpawn() {
		t.Fatal("codex is a subprocess provider — CanSpawn() should be true")
	}
}

func TestCodexSpawnArgsThreadsModel(t *testing.T) {
	p := NewCodexProvider()
	args, err := p.SpawnArgs(types.Pool{Model: "o3"}, "plan issue #1")
	if err != nil {
		t.Fatalf("SpawnArgs error: %v", err)
	}
	joined := strings.Join(args, " ")
	if !strings.Contains(joined, "--model o3") {
		t.Errorf("argv missing --model o3: %v", args)
	}
	if args[len(args)-1] != "plan issue #1" {
		t.Errorf("last arg = %q, want prompt", args[len(args)-1])
	}
	if args[0] != "codex" {
		t.Errorf("args[0] = %q, want codex", args[0])
	}
}

func TestCodexSpawnArgsOmitsEmptyModel(t *testing.T) {
	p := NewCodexProvider()
	args, err := p.SpawnArgs(types.Pool{}, "task")
	if err != nil {
		t.Fatalf("SpawnArgs error: %v", err)
	}
	for _, a := range args {
		if a == "--model" {
			t.Fatalf("--model present despite empty Pool.Model: %v", args)
		}
	}
}

func TestCodexSpawnArgsIncludesFullAutoApproval(t *testing.T) {
	p := NewCodexProvider()
	args, err := p.SpawnArgs(types.Pool{}, "task")
	if err != nil {
		t.Fatalf("SpawnArgs error: %v", err)
	}
	joined := strings.Join(args, " ")
	if !strings.Contains(joined, "--approval-mode full-auto") {
		t.Errorf("argv missing --approval-mode full-auto: %v", args)
	}
}

func TestCodexChatJSONNotSupported(t *testing.T) {
	p := NewCodexProvider()
	if _, err := p.ChatJSON(context.Background(), types.Pool{}, "sys", "user"); err != ErrNotSupported {
		t.Fatalf("ChatJSON returned %v, want ErrNotSupported", err)
	}
}

func TestCodexToolChatNotSupported(t *testing.T) {
	p := NewCodexProvider()
	if _, err := p.ToolChat(context.Background(), types.Pool{}, ChatRequest{}); err != ErrNotSupported {
		t.Fatalf("ToolChat returned %v, want ErrNotSupported", err)
	}
}

// TestCodexListModelsErrorsWhenBinaryMissing verifies that ListModels returns
// an actionable error when `codex` is not found in PATH. The test manipulates
// PATH to guarantee the binary is absent — robust against developer machines
// that happen to have codex installed.
func TestCodexListModelsErrorsWhenBinaryMissing(t *testing.T) {
	t.Setenv("PATH", "/nonexistent-empty-path")
	p := NewCodexProvider()
	_, err := p.ListModels(context.Background(), types.Pool{})
	if err == nil {
		t.Fatal("expected error when codex binary is absent, got nil")
	}
	if !strings.Contains(err.Error(), "not found in PATH") {
		t.Errorf("error message = %q, want 'not found in PATH' hint", err.Error())
	}
}

func TestCodexRegisteredInRegistry(t *testing.T) {
	r := NewRegistry(NewCodexProvider())
	p, ok := r.Get(types.ProviderCodex)
	if !ok {
		t.Fatal("codex provider not found in registry after registration")
	}
	if p.Kind() != types.ProviderCodex {
		t.Fatalf("Kind() = %s, want codex", p.Kind())
	}
}

func TestCodexImplementsProviderInterface(t *testing.T) {
	var _ Provider = NewCodexProvider()
}
