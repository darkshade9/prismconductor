package llm

import (
	"math"
	"strings"
)

// ModelRates holds the cost per million tokens for a specific model (issue #47).
type ModelRates struct {
	InputPerMillion  float64 `json:"input_per_million"`
	OutputPerMillion float64 `json:"output_per_million"`
}

// defaultModelRates is the built-in rate table keyed by model name prefix
// (lowercase). Rates are USD per million tokens as of 2025-05.
var defaultModelRates = map[string]ModelRates{
	// Claude 4
	"claude-opus-4":   {InputPerMillion: 15.0, OutputPerMillion: 75.0},
	"claude-sonnet-4": {InputPerMillion: 3.0, OutputPerMillion: 15.0},
	"claude-haiku-4":  {InputPerMillion: 0.25, OutputPerMillion: 1.25},
	// Claude 3.5
	"claude-opus-3-5":   {InputPerMillion: 15.0, OutputPerMillion: 75.0},
	"claude-sonnet-3-5": {InputPerMillion: 3.0, OutputPerMillion: 15.0},
	"claude-haiku-3-5":  {InputPerMillion: 0.8, OutputPerMillion: 4.0},
	// OpenAI
	"gpt-4.1":      {InputPerMillion: 2.0, OutputPerMillion: 8.0},
	"gpt-4.1-mini": {InputPerMillion: 0.4, OutputPerMillion: 1.6},
	"gpt-4.1-nano": {InputPerMillion: 0.1, OutputPerMillion: 0.4},
	"gpt-4o":       {InputPerMillion: 2.5, OutputPerMillion: 10.0},
	"gpt-4o-mini":  {InputPerMillion: 0.15, OutputPerMillion: 0.6},
	"gpt-4":        {InputPerMillion: 30.0, OutputPerMillion: 60.0},
	"o3":           {InputPerMillion: 10.0, OutputPerMillion: 40.0},
	"o4-mini":      {InputPerMillion: 1.1, OutputPerMillion: 4.4},
	// Gemini
	"gemini-2.5-pro":   {InputPerMillion: 1.25, OutputPerMillion: 10.0},
	"gemini-2.5-flash": {InputPerMillion: 0.15, OutputPerMillion: 0.6},
	"gemini-2.0-flash": {InputPerMillion: 0.1, OutputPerMillion: 0.4},
}

// LookupRates returns the best matching rates for a model string and whether
// a match was found. Exact match is tried first; prefix match (lowercased)
// is the fallback.
func LookupRates(model string) (ModelRates, bool) {
	if r, ok := defaultModelRates[model]; ok {
		return r, true
	}
	lower := strings.ToLower(model)
	for prefix, r := range defaultModelRates {
		if strings.HasPrefix(lower, prefix) {
			return r, true
		}
	}
	return ModelRates{}, false
}

// EstimateTokens returns an estimated token count for an execute session.
// Heuristic (Q1=A): base 5000 + 80 tokens per LOC + 5 tokens per plan word.
func EstimateTokens(totalLines, planWords int) int64 {
	return int64(5000 + totalLines*80 + planWords*5)
}

// EstimateCostUSD returns the estimated USD cost for a token count and rates.
// Treats all tokens as input (conservative, since output volume is unknown
// before execution). Result is rounded to 4 decimal places.
func EstimateCostUSD(tokens int64, rates ModelRates) float64 {
	raw := float64(tokens) * rates.InputPerMillion / 1_000_000
	return math.Round(raw*10000) / 10000
}

// IsFreeTierProvider reports whether the provider runs locally or without
// per-token billing. Ollama, LMStudio, and Codex (ChatGPT subscription CLI)
// never accrue per-token API costs. LiteLLM is a proxy that may front a paid
// backend, so it is not marked free here — actual spend will be $0 if the
// backend charges nothing. Gemini has both free and paid tiers; we leave it
// unmarked so that users who exceed the free quota see projected costs.
func IsFreeTierProvider(provider string) bool {
	switch provider {
	case "ollama", "lmstudio", "codex":
		return true
	}
	return false
}
