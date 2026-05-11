package llm

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"sort"
	"testing"

	"prismconductor/internal/types"
)

// TestOllamaListModels verifies that /api/tags JSON is parsed, model names
// are extracted, and the returned slice is sorted alphabetically.
func TestOllamaListModels(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/tags" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"models": []map[string]any{
				{"name": "llama3:latest"},
				{"name": "mistral:7b"},
				{"name": "codellama:13b"},
			},
		})
	}))
	defer ts.Close()

	p := ollamaProvider{client: &http.Client{}}
	pool := types.Pool{Endpoint: ts.URL}
	got, err := p.ListModels(context.Background(), pool)
	if err != nil {
		t.Fatalf("ListModels error: %v", err)
	}
	want := []string{"codellama:13b", "llama3:latest", "mistral:7b"}
	if len(got) != len(want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	if !sort.StringsAreSorted(got) {
		t.Fatalf("result not sorted: %v", got)
	}
	for i := range got {
		if got[i] != want[i] {
			t.Fatalf("got[%d] = %s, want %s", i, got[i], want[i])
		}
	}
}

// TestOllamaListModelsEmpty verifies that an empty models array produces an
// empty (not nil) slice with no error.
func TestOllamaListModelsEmpty(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{"models": []any{}})
	}))
	defer ts.Close()

	p := ollamaProvider{client: &http.Client{}}
	got, err := p.ListModels(context.Background(), types.Pool{Endpoint: ts.URL})
	if err != nil {
		t.Fatalf("ListModels error: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("expected empty slice, got %v", got)
	}
}

// TestOpenAIListModels verifies the /v1/models OpenAI envelope is parsed and
// model IDs are returned sorted.
func TestOpenAIListModels(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/models" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"data": []map[string]any{
				{"id": "gpt-4o"},
				{"id": "gpt-4o-mini"},
				{"id": "gpt-3.5-turbo"},
			},
		})
	}))
	defer ts.Close()

	p := openaiProvider{client: &http.Client{}}
	got, err := p.ListModels(context.Background(), types.Pool{Endpoint: ts.URL, APIKey: "test-key"})
	if err != nil {
		t.Fatalf("ListModels error: %v", err)
	}
	want := []string{"gpt-3.5-turbo", "gpt-4o", "gpt-4o-mini"}
	if len(got) != len(want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	if !sort.StringsAreSorted(got) {
		t.Fatalf("result not sorted: %v", got)
	}
	for i := range got {
		if got[i] != want[i] {
			t.Fatalf("got[%d] = %s, want %s", i, got[i], want[i])
		}
	}
}

// TestOpenAIListModelsUsesEnvKey verifies that when no API key is stored in
// the pool, the OPENAI_API_KEY environment variable is forwarded as the Bearer
// token.
func TestOpenAIListModelsUsesEnvKey(t *testing.T) {
	const envKey = "sk-test-env-key"
	t.Setenv("OPENAI_API_KEY", envKey)

	var gotAuth string
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"data": []map[string]any{{"id": "gpt-4o"}},
		})
	}))
	defer ts.Close()

	p := openaiProvider{client: &http.Client{}}
	_, err := p.ListModels(context.Background(), types.Pool{Endpoint: ts.URL, APIKey: ""})
	if err != nil {
		t.Fatalf("ListModels error: %v", err)
	}
	want := "Bearer " + envKey
	if gotAuth != want {
		t.Fatalf("Authorization header = %q, want %q", gotAuth, want)
	}

	// Restore env so tests don't leak state.
	os.Unsetenv("OPENAI_API_KEY")
}

// TestLiteLLMListModelsPrefers_model_info verifies that the /model/info
// LiteLLM-native endpoint is preferred and model_name fields are used.
func TestLiteLLMListModelsPrefers_model_info(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/model/info":
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]any{
				"data": []map[string]any{
					{"model_name": "gpt-4o-alias"},
					{"model_name": "claude-3-alias"},
				},
			})
		default:
			http.Error(w, "should not be called", http.StatusInternalServerError)
		}
	}))
	defer ts.Close()

	p := litellmProvider{client: &http.Client{}}
	got, err := p.ListModels(context.Background(), types.Pool{Endpoint: ts.URL})
	if err != nil {
		t.Fatalf("ListModels error: %v", err)
	}
	want := []string{"claude-3-alias", "gpt-4o-alias"}
	if len(got) != len(want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	for i := range got {
		if got[i] != want[i] {
			t.Fatalf("got[%d] = %s, want %s", i, got[i], want[i])
		}
	}
}

// TestLiteLLMListModelsFallsBackToV1Models verifies that when /model/info
// fails (error or empty), the fallback to /v1/models is used.
func TestLiteLLMListModelsFallsBackToV1Models(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/model/info":
			http.Error(w, "not found", http.StatusNotFound)
		case "/v1/models":
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]any{
				"data": []map[string]any{
					{"id": "mixtral-8x7b"},
					{"id": "llama-3-70b"},
				},
			})
		default:
			http.NotFound(w, r)
		}
	}))
	defer ts.Close()

	p := litellmProvider{client: &http.Client{}}
	got, err := p.ListModels(context.Background(), types.Pool{Endpoint: ts.URL})
	if err != nil {
		t.Fatalf("ListModels fallback error: %v", err)
	}
	want := []string{"llama-3-70b", "mixtral-8x7b"}
	if len(got) != len(want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	for i := range got {
		if got[i] != want[i] {
			t.Fatalf("got[%d] = %s, want %s", i, got[i], want[i])
		}
	}
}

// TestLMStudioListModelsNativeAPI verifies the /api/v0/models native endpoint
// is used and only models with state="loaded" (or empty state) are included.
func TestLMStudioListModelsNativeAPI(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v0/models":
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]any{
				"data": []map[string]any{
					{"id": "qwen2.5-coder-7b", "state": "loaded"},
					{"id": "llama-3.2-1b", "state": "loaded"},
					{"id": "phi-4", "state": "not-loaded"},
					{"id": "gemma-2-2b", "state": ""},
				},
			})
		default:
			http.Error(w, "fallback should not be hit", http.StatusInternalServerError)
		}
	}))
	defer ts.Close()

	p := lmstudioProvider{client: &http.Client{}}
	got, err := p.ListModels(context.Background(), types.Pool{Endpoint: ts.URL})
	if err != nil {
		t.Fatalf("ListModels error: %v", err)
	}
	// "phi-4" (state=not-loaded) must be excluded; empty-state entries included.
	want := []string{"gemma-2-2b", "llama-3.2-1b", "qwen2.5-coder-7b"}
	if len(got) != len(want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	for i := range got {
		if got[i] != want[i] {
			t.Fatalf("got[%d] = %s, want %s", i, got[i], want[i])
		}
	}
}

// TestLMStudioListModelsFallback verifies that when /api/v0/models returns an
// HTTP error, the provider falls back to /v1/models.
func TestLMStudioListModelsFallback(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v0/models":
			http.Error(w, "service unavailable", http.StatusServiceUnavailable)
		case "/v1/models":
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]any{
				"data": []map[string]any{
					{"id": "meta-llama-3.1-8b-instruct"},
					{"id": "deepseek-r1-distill-qwen-7b"},
				},
			})
		default:
			http.NotFound(w, r)
		}
	}))
	defer ts.Close()

	p := lmstudioProvider{client: &http.Client{}}
	got, err := p.ListModels(context.Background(), types.Pool{Endpoint: ts.URL})
	if err != nil {
		t.Fatalf("ListModels fallback error: %v", err)
	}
	want := []string{"deepseek-r1-distill-qwen-7b", "meta-llama-3.1-8b-instruct"}
	if len(got) != len(want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	for i := range got {
		if got[i] != want[i] {
			t.Fatalf("got[%d] = %s, want %s", i, got[i], want[i])
		}
	}
}
