package llm

import (
	"encoding/json"
	"errors"
	"testing"
	"time"

	anyllmerrors "github.com/mozilla-ai/any-llm-go/errors"

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

// TestGeminiAPIKeyHelpURL verifies that Gemini advertises its API key URL
// while other providers return an empty string.
func TestGeminiAPIKeyHelpURL(t *testing.T) {
	g := NewGeminiProvider()
	if g.APIKeyHelpURL() != "https://aistudio.google.com/apikey" {
		t.Fatalf("Gemini APIKeyHelpURL = %q, want aistudio URL", g.APIKeyHelpURL())
	}
	if NewClaudeProvider().APIKeyHelpURL() != "" {
		t.Fatal("Claude APIKeyHelpURL should be empty")
	}
}

// TestWrapQuotaError verifies that RateLimitError is converted to QuotaExceededError
// and that a non-rate-limit error passes through unchanged.
func TestWrapQuotaError(t *testing.T) {
	t.Run("rate limit becomes QuotaExceededError", func(t *testing.T) {
		rle := anyllmerrors.NewRateLimitError("gemini", errors.New("429 RESOURCE_EXHAUSTED"))
		got := wrapQuotaError(rle)
		var qe *QuotaExceededError
		if !errors.As(got, &qe) {
			t.Fatalf("wrapQuotaError(%T) = %T, want *QuotaExceededError", rle, got)
		}
		if qe.Provider != "gemini" {
			t.Errorf("QuotaExceededError.Provider = %q, want %q", qe.Provider, "gemini")
		}
	})

	t.Run("rate limit with RetryAfter seconds", func(t *testing.T) {
		before := time.Now()
		rle := anyllmerrors.NewRateLimitError("gemini", errors.New("429"))
		rle.RetryAfter = 3600
		got := wrapQuotaError(rle)
		var qe *QuotaExceededError
		if !errors.As(got, &qe) {
			t.Fatalf("expected *QuotaExceededError, got %T", got)
		}
		if qe.RetryAfter.IsZero() {
			t.Fatal("QuotaExceededError.RetryAfter should be set when RetryAfter seconds > 0")
		}
		if !qe.RetryAfter.After(before) {
			t.Errorf("RetryAfter %v is not after %v", qe.RetryAfter, before)
		}
	})

	t.Run("non-rate-limit error passes through", func(t *testing.T) {
		orig := errors.New("some other error")
		got := wrapQuotaError(orig)
		if got != orig {
			t.Fatalf("wrapQuotaError(non-rate-limit) = %v, want original error", got)
		}
	})

	t.Run("nil returns nil", func(t *testing.T) {
		if wrapQuotaError(nil) != nil {
			t.Fatal("wrapQuotaError(nil) should return nil")
		}
	})
}
