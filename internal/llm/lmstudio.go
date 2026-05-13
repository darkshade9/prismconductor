package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"sort"
	"strings"

	"prismconductor/internal/types"
)

const defaultLMStudioEndpoint = "http://localhost:1234"

type lmstudioProvider struct {
	client *http.Client
}

func NewLMStudioProvider() Provider {
	return lmstudioProvider{client: NewHTTPClient(DefaultHTTPTimeout)}
}

func (lmstudioProvider) Kind() types.Provider    { return types.ProviderLMStudio }
func (lmstudioProvider) DisplayName() string     { return "LM Studio" }
func (lmstudioProvider) DefaultEndpoint() string { return defaultLMStudioEndpoint }
func (lmstudioProvider) NeedsAPIKey() bool       { return true }
func (lmstudioProvider) APIKeyHelpURL() string   { return "" }
func (lmstudioProvider) CanSpawn() bool          { return true }

// ListModels prefers LM Studio's /api/v0/models (native — includes load state)
// filtering to loaded entries, falling back to OpenAI-compat /v1/models when
// v0 isn't reachable.
func (l lmstudioProvider) ListModels(ctx context.Context, p types.Pool) ([]string, error) {
	endpoint := strings.TrimRight(p.Endpoint, "/")
	if endpoint == "" {
		endpoint = defaultLMStudioEndpoint
	}
	key := p.APIKey

	if body, err := httpGetWithBearer(ctx, l.client, endpoint+"/api/v0/models", key); err == nil {
		var out struct {
			Data []struct {
				ID    string `json:"id"`
				State string `json:"state"`
			} `json:"data"`
		}
		if err := json.Unmarshal(body, &out); err == nil {
			ids := make([]string, 0, len(out.Data))
			for _, m := range out.Data {
				if m.ID == "" {
					continue
				}
				if m.State != "" && m.State != "loaded" {
					continue
				}
				ids = append(ids, m.ID)
			}
			sort.Strings(ids)
			return ids, nil
		}
	}

	body, err := httpGetWithBearer(ctx, l.client, modelsURL(endpoint), key)
	if err != nil {
		return nil, err
	}
	return decodeOpenAIModels(bytes.NewReader(body))
}

func (lmstudioProvider) SpawnArgs(_ types.Pool, _ string) ([]string, error) {
	return nil, ErrNotSupported
}

func (l lmstudioProvider) ChatJSON(ctx context.Context, p types.Pool, system, user string) (string, error) {
	endpoint := l.resolveEndpoint(p)
	return openAICompatChat(ctx, l.client, endpoint, p.APIKey, p.Model, system, user, p.Temperature, nil)
}

// ToolChat drives a tool-using turn through LM Studio's OpenAI-compat endpoint
// (issue #58). LM Studio's tool-calling support varies by loaded model; for
// models without native tool tokens the server may emit content only and the
// harness treats that as a clean stop.
func (l lmstudioProvider) ToolChat(ctx context.Context, p types.Pool, req ChatRequest) (ChatResponse, error) {
	endpoint := l.resolveEndpoint(p)
	return openAIToolChat(ctx, l.client, endpoint, p.APIKey, p.Model, req, p.Temperature, nil)
}

func (l lmstudioProvider) resolveEndpoint(p types.Pool) string {
	endpoint := strings.TrimRight(p.Endpoint, "/")
	if endpoint == "" {
		endpoint = defaultLMStudioEndpoint
	}
	return endpoint
}
