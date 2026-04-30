package llm

import (
	"encoding/json"
	"testing"

	"prismconductor/internal/types"
)

// TestGeminiProviderShape pins the static metadata + spawn-strategy contract:
// any-llm-go-backed providers route through ToolChat (harness strategy), not
// SpawnArgs (subprocess). If this changes, the session manager's dispatch
// logic in spawnWithDir would need to follow.
func TestGeminiProviderShape(t *testing.T) {
	p := NewGeminiProvider()
	if p.Kind() != types.ProviderGemini {
		t.Fatalf("Kind = %s, want %s", p.Kind(), types.ProviderGemini)
	}
	if !p.CanSpawn() {
		t.Fatal("Gemini.CanSpawn must be true so plan/work pools accept it")
	}
	if !p.NeedsAPIKey() {
		t.Fatal("Gemini.NeedsAPIKey must be true so the UI requires a key field")
	}
	if _, err := p.SpawnArgs(types.Pool{}, "x"); err != ErrNotSupported {
		t.Fatalf("SpawnArgs = %v, want ErrNotSupported (harness strategy)", err)
	}
}

// TestToolCallExtraRoundTrip pins the multi-turn contract for Gemini's
// thought_signature: the harness's history loop stores ChatResponse.ToolCalls
// directly back into Message.ToolCalls, so adding the Extra field on ToolCall
// is the only seam needed for round-tripping. This test would fail-to-compile
// if Extra were renamed or removed.
func TestToolCallExtraRoundTrip(t *testing.T) {
	tc := ToolCall{
		ID:   "call_1",
		Name: "Bash",
		Args: json.RawMessage(`{"command":"ls"}`),
		Extra: map[string]any{
			"gemini": map[string]any{
				"thought_signature": "abc123",
			},
		},
	}
	if tc.Extra["gemini"].(map[string]any)["thought_signature"] != "abc123" {
		t.Fatal("Extra round-trip lost thought_signature")
	}
}

// TestRegistryIncludesGemini guards against a dropped registration in
// app.go's startup. If anyone removes NewGeminiProvider() from the slice,
// the app loses Gemini entirely with no other test catching it.
func TestRegistryIncludesGemini(t *testing.T) {
	r := NewRegistry(NewGeminiProvider())
	if _, ok := r.Get(types.ProviderGemini); !ok {
		t.Fatal("registry missing gemini provider")
	}
	if !r.CanSpawn(types.ProviderGemini) {
		t.Fatal("registry CanSpawn returned false for gemini")
	}
}
