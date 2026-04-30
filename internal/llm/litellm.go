package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"os"
	"sort"
	"strings"
	"time"

	"prismconductor/internal/types"
)

const defaultLiteLLMEndpoint = "http://localhost:4000"

type litellmProvider struct {
	client *http.Client
}

func NewLiteLLMProvider() Provider {
	return litellmProvider{client: &http.Client{Timeout: 5 * time.Minute}}
}

func (litellmProvider) Kind() types.Provider    { return types.ProviderLiteLLM }
func (litellmProvider) DisplayName() string     { return "LiteLLM" }
func (litellmProvider) DefaultEndpoint() string { return defaultLiteLLMEndpoint }
func (litellmProvider) NeedsAPIKey() bool       { return true }
func (litellmProvider) CanSpawn() bool          { return true }

// ListModels prefers /model/info (LiteLLM-native — exposes alias names), falls
// back to /v1/models on any failure.
func (l litellmProvider) ListModels(ctx context.Context, p types.Pool) ([]string, error) {
	endpoint := strings.TrimRight(p.Endpoint, "/")
	if endpoint == "" {
		endpoint = defaultLiteLLMEndpoint
	}
	key := p.APIKey
	if key == "" {
		key = os.Getenv("LITELLM_API_KEY")
	}

	if body, err := httpGetWithBearer(ctx, l.client, endpoint+"/model/info", key); err == nil {
		var out struct {
			Data []struct {
				ModelName string `json:"model_name"`
			} `json:"data"`
		}
		if err := json.Unmarshal(body, &out); err == nil && len(out.Data) > 0 {
			ids := make([]string, 0, len(out.Data))
			for _, m := range out.Data {
				if m.ModelName != "" {
					ids = append(ids, m.ModelName)
				}
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

func (litellmProvider) SpawnArgs(_ types.Pool, _ string) ([]string, error) {
	return nil, ErrNotSupported
}

func (l litellmProvider) ChatJSON(ctx context.Context, p types.Pool, system, user string) (string, error) {
	endpoint, key := l.resolve(p)
	return openAICompatChat(ctx, l.client, endpoint, key, p.Model, system, user)
}

// ToolChat drives a tool-using turn through LiteLLM's OpenAI-compat endpoint
// (issue #58). LiteLLM forwards `tools[]` to the backing model; whether the
// model emits tool_calls depends on the backend.
func (l litellmProvider) ToolChat(ctx context.Context, p types.Pool, req ChatRequest) (ChatResponse, error) {
	endpoint, key := l.resolve(p)
	return openAIToolChat(ctx, l.client, endpoint, key, p.Model, req)
}

func (l litellmProvider) resolve(p types.Pool) (string, string) {
	endpoint := strings.TrimRight(p.Endpoint, "/")
	if endpoint == "" {
		endpoint = defaultLiteLLMEndpoint
	}
	key := p.APIKey
	if key == "" {
		key = os.Getenv("LITELLM_API_KEY")
	}
	return endpoint, key
}
