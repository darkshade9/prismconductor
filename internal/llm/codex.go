package llm

import (
	"context"
	"fmt"
	"os/exec"
	"strings"
	"time"

	"prismconductor/internal/types"
)

// codexKnownModels is the static list of models available via the codex CLI.
// The CLI does not expose a listing API; models are enumerated from OpenAI
// documentation. Update via /refresh-model-hints when new models ship.
var codexKnownModels = []string{
	"o3",
	"o4-mini",
	"gpt-4o",
	"gpt-4.1",
	"gpt-4.1-mini",
}

type codexProvider struct{}

func NewCodexProvider() Provider {
	return codexProvider{}
}

func (codexProvider) Kind() types.Provider    { return types.ProviderCodex }
func (codexProvider) DisplayName() string     { return "Codex CLI" }
func (codexProvider) DefaultEndpoint() string { return "" }
func (codexProvider) NeedsAPIKey() bool       { return false }
func (codexProvider) CanSpawn() bool          { return true }

// ListModels probes the local codex binary. If the binary is not found in
// PATH, ListModels returns an error so the UI's "Test connection" button
// surfaces the install hint. When the binary is found, the static model list
// is returned — codex has no model-listing API.
func (codexProvider) ListModels(ctx context.Context, _ types.Pool) ([]string, error) {
	path, err := exec.LookPath("codex")
	if err != nil {
		return nil, fmt.Errorf("codex binary not found in PATH — install it with `npm install -g @openai/codex` and run `codex login`")
	}
	// Quick version probe with a short timeout so "Test connection" is snappy.
	probeCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	out, err := exec.CommandContext(probeCtx, path, "--version").CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("codex --version failed (%w); run `codex login` to authenticate", err)
	}
	_ = strings.TrimSpace(string(out)) // version string, unused but confirms binary is healthy
	return codexKnownModels, nil
}

// SpawnArgs returns the argv for the codex subprocess. Mirrors the Claude
// pattern: the prompt is the last positional argument, and the model is
// threaded through --model when set on the pool. --approval-mode full-auto
// suppresses interactive permission prompts so the conductor's PTY tail can
// parse output without waiting for user interaction.
func (codexProvider) SpawnArgs(p types.Pool, prompt string) ([]string, error) {
	args := []string{
		"codex",
		"--approval-mode", "full-auto",
	}
	if p.Model != "" {
		args = append(args, "--model", p.Model)
	}
	return append(args, prompt), nil
}

// ChatJSON is unsupported for codex pools — inference runs through the local
// CLI subprocess, not an HTTP endpoint. The orchestrator's rank+deps call
// should route to a non-codex orchestrator pool.
func (codexProvider) ChatJSON(_ context.Context, _ types.Pool, _, _ string) (string, error) {
	return "", ErrNotSupported
}

// ToolChat is unsupported for codex pools — codex runs as a subprocess and
// has its own internal tool loop. The harness strategy does not apply.
func (codexProvider) ToolChat(_ context.Context, _ types.Pool, _ ChatRequest) (ChatResponse, error) {
	return ChatResponse{}, ErrNotSupported
}
