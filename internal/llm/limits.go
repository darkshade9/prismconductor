package llm

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"time"

	"prismconductor/internal/types"
)

// ParseOpenAIRateLimitHeaders inspects an OpenAI response's headers and
// returns PoolUsage snapshots for any rate-limit windows it finds.
// Returns nil if no rate-limit headers are present.
//
// OpenAI sends two independent windows: requests and tokens. Each yields one
// PoolUsage row keyed by (poolID, window).
func ParseOpenAIRateLimitHeaders(h http.Header, poolID, poolName string) []types.PoolUsage {
	now := time.Now()
	var out []types.PoolUsage

	// Requests window
	if lim := parseInt64(h.Get("x-ratelimit-limit-requests")); lim > 0 {
		rem := parseInt64(h.Get("x-ratelimit-remaining-requests"))
		reset := parseResetTime(h.Get("x-ratelimit-reset-requests"), now)
		out = append(out, types.PoolUsage{
			PoolID:     poolID,
			PoolName:   poolName,
			Provider:   string(types.ProviderOpenAI),
			Window:     "requests",
			LimitValue: lim,
			Used:       lim - rem,
			ResetsAt:   reset,
			CapturedAt: now,
		})
	}

	// Tokens window
	if lim := parseInt64(h.Get("x-ratelimit-limit-tokens")); lim > 0 {
		rem := parseInt64(h.Get("x-ratelimit-remaining-tokens"))
		reset := parseResetTime(h.Get("x-ratelimit-reset-tokens"), now)
		out = append(out, types.PoolUsage{
			PoolID:     poolID,
			PoolName:   poolName,
			Provider:   string(types.ProviderOpenAI),
			Window:     "tokens",
			LimitValue: lim,
			Used:       lim - rem,
			ResetsAt:   reset,
			CapturedAt: now,
		})
	}

	return out
}

// ParseClaudeRateLimitEvent inspects a single stream-json line from a Claude
// Code session. If the line is a rate_limit system event, it returns the
// corresponding PoolUsage snapshots and true. Otherwise returns nil, false.
//
// Claude Code CLI emits rate-limit info as a system event:
//
//	{"type":"system","subtype":"rate_limit","rate_limit":{
//	  "requests_limit":500,"requests_remaining":490,
//	  "requests_reset":"2024-01-01T00:05:00Z",
//	  "input_tokens_limit":50000,"input_tokens_remaining":45000,
//	  "input_tokens_reset":"2024-01-01T00:05:00Z",
//	  "output_tokens_limit":10000,"output_tokens_remaining":9000,
//	  "output_tokens_reset":"2024-01-01T00:05:00Z"
//	}}
func ParseClaudeRateLimitEvent(raw string, poolID, poolName string) ([]types.PoolUsage, bool) {
	if !strings.Contains(raw, `"rate_limit"`) {
		return nil, false
	}

	var env struct {
		Type    string `json:"type"`
		Subtype string `json:"subtype"`
		Data    struct {
			RequestsLimit          int64  `json:"requests_limit"`
			RequestsRemaining      int64  `json:"requests_remaining"`
			RequestsReset          string `json:"requests_reset"`
			InputTokensLimit       int64  `json:"input_tokens_limit"`
			InputTokensRemaining   int64  `json:"input_tokens_remaining"`
			InputTokensReset       string `json:"input_tokens_reset"`
			OutputTokensLimit      int64  `json:"output_tokens_limit"`
			OutputTokensRemaining  int64  `json:"output_tokens_remaining"`
			OutputTokensReset      string `json:"output_tokens_reset"`
		} `json:"rate_limit"`
	}

	if err := json.Unmarshal([]byte(raw), &env); err != nil {
		return nil, false
	}
	if env.Type != "system" || env.Subtype != "rate_limit" {
		return nil, false
	}

	now := time.Now()
	var out []types.PoolUsage

	if env.Data.RequestsLimit > 0 {
		reset, _ := time.Parse(time.RFC3339, env.Data.RequestsReset)
		if reset.IsZero() {
			reset = now.Add(5 * time.Minute)
		}
		out = append(out, types.PoolUsage{
			PoolID:     poolID,
			PoolName:   poolName,
			Provider:   string(types.ProviderClaude),
			Window:     "requests",
			LimitValue: env.Data.RequestsLimit,
			Used:       env.Data.RequestsLimit - env.Data.RequestsRemaining,
			ResetsAt:   reset,
			CapturedAt: now,
		})
	}

	totalTokenLimit := env.Data.InputTokensLimit + env.Data.OutputTokensLimit
	totalTokenUsed := (env.Data.InputTokensLimit - env.Data.InputTokensRemaining) +
		(env.Data.OutputTokensLimit - env.Data.OutputTokensRemaining)
	if totalTokenLimit > 0 {
		reset, _ := time.Parse(time.RFC3339, env.Data.InputTokensReset)
		if reset.IsZero() {
			reset = now.Add(5 * time.Minute)
		}
		out = append(out, types.PoolUsage{
			PoolID:     poolID,
			PoolName:   poolName,
			Provider:   string(types.ProviderClaude),
			Window:     "tokens",
			LimitValue: totalTokenLimit,
			Used:       totalTokenUsed,
			ResetsAt:   reset,
			CapturedAt: now,
		})
	}

	if len(out) == 0 {
		return nil, false
	}
	return out, true
}

// parseInt64 parses a header value as int64; returns 0 on failure.
func parseInt64(s string) int64 {
	if s == "" {
		return 0
	}
	n, _ := strconv.ParseInt(strings.TrimSpace(s), 10, 64)
	return n
}

// parseResetTime converts an OpenAI reset header (e.g. "6m0s", "1h30m0s", or
// RFC3339) into an absolute time. Falls back to now+1h on parse failure.
func parseResetTime(s string, now time.Time) time.Time {
	if s == "" {
		return now.Add(time.Hour)
	}
	// Try RFC3339 first.
	if t, err := time.Parse(time.RFC3339, s); err == nil {
		return t
	}
	// Try Go duration (OpenAI sometimes sends "6m0s").
	if d, err := time.ParseDuration(s); err == nil {
		return now.Add(d)
	}
	return now.Add(time.Hour)
}
