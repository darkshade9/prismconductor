package llm

import (
	"bytes"
	"context"
	"net/http"
	"os"
	"strings"
	"time"

	"prismconductor/internal/types"
)

const defaultOpenAIEndpoint = "https://api.openai.com/v1"

type openaiProvider struct {
	client *http.Client
}

func NewOpenAIProvider() Provider {
	return openaiProvider{client: &http.Client{Timeout: 30 * time.Second}}
}

func (openaiProvider) Kind() types.Provider    { return types.ProviderOpenAI }
func (openaiProvider) DisplayName() string     { return "OpenAI" }
func (openaiProvider) DefaultEndpoint() string { return defaultOpenAIEndpoint }
func (openaiProvider) NeedsAPIKey() bool       { return true }
func (openaiProvider) CanSpawn() bool          { return false }

func (o openaiProvider) ListModels(ctx context.Context, p types.Pool) ([]string, error) {
	endpoint := strings.TrimRight(p.Endpoint, "/")
	if endpoint == "" {
		endpoint = defaultOpenAIEndpoint
	}
	key := p.APIKey
	if key == "" {
		key = os.Getenv("OPENAI_API_KEY")
	}
	body, err := httpGetWithBearer(ctx, o.client, endpoint+"/models", key)
	if err != nil {
		return nil, err
	}
	return decodeOpenAIModels(bytes.NewReader(body))
}

func (openaiProvider) SpawnArgs(_ types.Pool, _ string) ([]string, error) {
	return nil, ErrNotSupported
}
