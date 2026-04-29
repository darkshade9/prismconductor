package llm

import (
	"context"
	"encoding/json"
	"net/http"
	"sort"
	"strings"
	"time"

	"prismconductor/internal/types"
)

const defaultOllamaEndpoint = "http://localhost:11434"

type ollamaProvider struct {
	client *http.Client
}

func NewOllamaProvider() Provider {
	return ollamaProvider{client: &http.Client{Timeout: 30 * time.Second}}
}

func (ollamaProvider) Kind() types.Provider    { return types.ProviderOllama }
func (ollamaProvider) DisplayName() string     { return "Ollama" }
func (ollamaProvider) DefaultEndpoint() string { return defaultOllamaEndpoint }
func (ollamaProvider) NeedsAPIKey() bool       { return true }
func (ollamaProvider) CanSpawn() bool          { return false }

// ListModels parses Ollama's /api/tags. Bearer auth is forwarded so a tunneled
// (e.g. Tailscale Funnel) endpoint guarded by HTTP-basic-equivalent auth still
// works.
func (o ollamaProvider) ListModels(ctx context.Context, p types.Pool) ([]string, error) {
	endpoint := strings.TrimRight(p.Endpoint, "/")
	if endpoint == "" {
		endpoint = defaultOllamaEndpoint
	}
	body, err := httpGetWithBearer(ctx, o.client, endpoint+"/api/tags", p.APIKey)
	if err != nil {
		return nil, err
	}
	var out struct {
		Models []struct {
			Name string `json:"name"`
		} `json:"models"`
	}
	if err := json.Unmarshal(body, &out); err != nil {
		return nil, err
	}
	ids := make([]string, 0, len(out.Models))
	for _, m := range out.Models {
		if m.Name != "" {
			ids = append(ids, m.Name)
		}
	}
	sort.Strings(ids)
	return ids, nil
}

func (ollamaProvider) SpawnArgs(_ types.Pool, _ string) ([]string, error) {
	return nil, ErrNotSupported
}

// ChatJSON routes through Ollama's OpenAI-compat /v1/chat/completions. The
// extended timeout in NewOllamaProvider is short for orchestrator runs against
// large local models, so callers should pass a context with their own
// deadline (the orchestrator gives 5 minutes).
func (o ollamaProvider) ChatJSON(ctx context.Context, p types.Pool, system, user string) (string, error) {
	endpoint := strings.TrimRight(p.Endpoint, "/")
	if endpoint == "" {
		endpoint = defaultOllamaEndpoint
	}
	return openAICompatChat(ctx, o.client, endpoint, p.APIKey, p.Model, system, user)
}
