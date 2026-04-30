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

// defaultOpenAIEndpoint is the bare host. The `/v1` segment is added by
// `chatCompletionsURL` (orchestrator_chat.go) and by ListModels below; keeping
// it out of the constant means a user pasting `https://api.openai.com/v1` into
// the pool editor doesn't double-prefix into `…/v1/v1/chat/completions`.
const defaultOpenAIEndpoint = "https://api.openai.com"

type openaiProvider struct {
	client *http.Client
}

func NewOpenAIProvider() Provider {
	return openaiProvider{client: &http.Client{Timeout: 5 * time.Minute}}
}

func (openaiProvider) Kind() types.Provider    { return types.ProviderOpenAI }
func (openaiProvider) DisplayName() string     { return "OpenAI" }
func (openaiProvider) DefaultEndpoint() string { return defaultOpenAIEndpoint }
func (openaiProvider) NeedsAPIKey() bool       { return true }
func (openaiProvider) CanSpawn() bool          { return true }

func (o openaiProvider) ListModels(ctx context.Context, p types.Pool) ([]string, error) {
	endpoint := strings.TrimRight(p.Endpoint, "/")
	if endpoint == "" {
		endpoint = defaultOpenAIEndpoint
	}
	key := p.APIKey
	if key == "" {
		key = os.Getenv("OPENAI_API_KEY")
	}
	body, err := httpGetWithBearer(ctx, o.client, modelsURL(endpoint), key)
	if err != nil {
		return nil, err
	}
	return decodeOpenAIModels(bytes.NewReader(body))
}

func (openaiProvider) SpawnArgs(_ types.Pool, _ string) ([]string, error) {
	return nil, ErrNotSupported
}

// ChatJSON routes through OpenAI's /v1/chat/completions. APIKey falls back to
// OPENAI_API_KEY so existing operator setups keep working.
func (o openaiProvider) ChatJSON(ctx context.Context, p types.Pool, system, user string) (string, error) {
	endpoint, key := o.resolve(p)
	return openAICompatChat(ctx, o.client, endpoint, key, p.Model, system, user)
}

// ToolChat drives a tool-using turn against OpenAI's /v1/chat/completions
// (issue #58). Used by the harness when SpawnArgs returns ErrNotSupported.
func (o openaiProvider) ToolChat(ctx context.Context, p types.Pool, req ChatRequest) (ChatResponse, error) {
	endpoint, key := o.resolve(p)
	return openAIToolChat(ctx, o.client, endpoint, key, p.Model, req)
}

func (o openaiProvider) resolve(p types.Pool) (string, string) {
	endpoint := strings.TrimRight(p.Endpoint, "/")
	if endpoint == "" {
		endpoint = defaultOpenAIEndpoint
	}
	key := p.APIKey
	if key == "" {
		key = os.Getenv("OPENAI_API_KEY")
	}
	return endpoint, key
}
