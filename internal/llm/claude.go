package llm

import (
	"context"

	"prismconductor/internal/types"
)

type claudeProvider struct{}

func NewClaudeProvider() Provider { return claudeProvider{} }

func (claudeProvider) Kind() types.Provider    { return types.ProviderClaude }
func (claudeProvider) DisplayName() string     { return "Claude" }
func (claudeProvider) DefaultEndpoint() string { return "" }
func (claudeProvider) NeedsAPIKey() bool       { return false }
func (claudeProvider) CanSpawn() bool          { return true }

// Claude has no listing API; CLAUDE.md "Claude 4.X" family is the canonical
// set. The CLI accepts arbitrary --model IDs so the user can still type a
// preview ID into the modal.
func (claudeProvider) ListModels(_ context.Context, _ types.Pool) ([]string, error) {
	return []string{
		"claude-opus-4-7",
		"claude-sonnet-4-6",
		"claude-haiku-4-5-20251001",
	}, nil
}

// SpawnArgs mirrors the original session/manager.go:claudeArgs helper, threading
// pool.Model through --model when set.
func (claudeProvider) SpawnArgs(p types.Pool, prompt string) ([]string, error) {
	args := []string{
		"claude",
		"-p",
		"--output-format", "stream-json",
		"--include-partial-messages",
		"--verbose",
		"--permission-mode", "bypassPermissions",
	}
	if p.Model != "" {
		args = append(args, "--model", p.Model)
	}
	return append(args, prompt), nil
}
