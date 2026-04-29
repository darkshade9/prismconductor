package orchestrator

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
	"text/template"

	"prismconductor/internal/types"
)

// IssueExcerpt is the truncated view of an issue used in the rank+deps prompt.
// Body is capped at 800 chars per §11.2.
type IssueExcerpt struct {
	Number      int
	Title       string
	BodyExcerpt string
}

// RankDepsInput is what we render into UserPromptRankDepsTemplate.
type RankDepsInput struct {
	Goal   types.Goal
	Issues []IssueExcerpt
}

// LLM is the slice of ollama.Client we need (decoupled for testability).
type LLM interface {
	Generate(ctx context.Context, system, prompt string) (string, error)
}

// RankDeps renders the prompt, calls the LLM, and parses the response into a
// RankDepsResult. Returns an error if the model output isn't valid JSON.
func RankDeps(ctx context.Context, llm LLM, goal types.Goal, issues []types.Issue) (*RankDepsResult, error) {
	if llm == nil {
		return nil, fmt.Errorf("LLM not configured")
	}
	if len(issues) == 0 {
		return &RankDepsResult{}, nil
	}
	excerpts := make([]IssueExcerpt, 0, len(issues))
	for _, iss := range issues {
		excerpts = append(excerpts, IssueExcerpt{
			Number:      iss.Number,
			Title:       iss.Title,
			BodyExcerpt: truncate(iss.Body, 800),
		})
	}

	tmpl, err := template.New("rankdeps").Parse(UserPromptRankDepsTemplate)
	if err != nil {
		return nil, fmt.Errorf("template parse: %w", err)
	}
	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, RankDepsInput{Goal: goal, Issues: excerpts}); err != nil {
		return nil, fmt.Errorf("template execute: %w", err)
	}

	raw, err := llm.Generate(ctx, SystemPromptRankDeps, buf.String())
	if err != nil {
		return nil, err
	}
	cleaned := stripFences(raw)

	var out RankDepsResult
	if err := json.Unmarshal([]byte(cleaned), &out); err != nil {
		return nil, fmt.Errorf("invalid JSON from LLM: %w; raw: %s", err, snippet(cleaned, 200))
	}
	return &out, nil
}

// IssueBodyHash returns a stable hash of an issue's title+body+labels for caching.
func IssueBodyHash(iss types.Issue) string {
	h := sha256.New()
	h.Write([]byte(iss.Title))
	h.Write([]byte{0})
	h.Write([]byte(iss.Body))
	h.Write([]byte{0})
	for _, l := range iss.Labels {
		h.Write([]byte(l))
		h.Write([]byte{0})
	}
	return hex.EncodeToString(h.Sum(nil))
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}

// stripFences removes the rare ```json fence the model emits despite the
// "no markdown fences" instruction.
func stripFences(s string) string {
	s = strings.TrimSpace(s)
	if strings.HasPrefix(s, "```") {
		// drop first line
		if i := strings.Index(s, "\n"); i >= 0 {
			s = s[i+1:]
		}
		if j := strings.LastIndex(s, "```"); j >= 0 {
			s = s[:j]
		}
	}
	return strings.TrimSpace(s)
}

func snippet(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}
