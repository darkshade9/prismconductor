package llm

import (
	"net/http"
	"testing"
	"time"
)

func TestParseOpenAIRateLimitHeaders_BothWindows(t *testing.T) {
	h := http.Header{}
	h.Set("x-ratelimit-limit-requests", "500")
	h.Set("x-ratelimit-remaining-requests", "490")
	h.Set("x-ratelimit-reset-requests", "6m0s")
	h.Set("x-ratelimit-limit-tokens", "100000")
	h.Set("x-ratelimit-remaining-tokens", "80000")
	h.Set("x-ratelimit-reset-tokens", "1m30s")

	usages := ParseOpenAIRateLimitHeaders(h, "pool-1", "MyPool")
	if len(usages) != 2 {
		t.Fatalf("expected 2 usage rows, got %d", len(usages))
	}

	req := usages[0]
	if req.Window != "requests" {
		t.Errorf("window = %q, want requests", req.Window)
	}
	if req.LimitValue != 500 {
		t.Errorf("limit = %d, want 500", req.LimitValue)
	}
	if req.Used != 10 { // 500 - 490
		t.Errorf("used = %d, want 10", req.Used)
	}
	if req.PoolID != "pool-1" {
		t.Errorf("pool_id = %q, want pool-1", req.PoolID)
	}

	tok := usages[1]
	if tok.Window != "tokens" {
		t.Errorf("window = %q, want tokens", tok.Window)
	}
	if tok.LimitValue != 100000 {
		t.Errorf("limit = %d, want 100000", tok.LimitValue)
	}
	if tok.Used != 20000 { // 100000 - 80000
		t.Errorf("used = %d, want 20000", tok.Used)
	}
}

func TestParseOpenAIRateLimitHeaders_Empty(t *testing.T) {
	usages := ParseOpenAIRateLimitHeaders(http.Header{}, "pool-1", "MyPool")
	if len(usages) != 0 {
		t.Errorf("expected 0 usages for empty headers, got %d", len(usages))
	}
}

func TestParseOpenAIRateLimitHeaders_RequestsOnly(t *testing.T) {
	h := http.Header{}
	h.Set("x-ratelimit-limit-requests", "100")
	h.Set("x-ratelimit-remaining-requests", "95")

	usages := ParseOpenAIRateLimitHeaders(h, "pool-1", "MyPool")
	if len(usages) != 1 || usages[0].Window != "requests" {
		t.Errorf("expected exactly 1 requests row, got %v", usages)
	}
}

func TestParseClaudeRateLimitEvent_Valid(t *testing.T) {
	raw := `{"type":"system","subtype":"rate_limit","rate_limit":{"requests_limit":500,"requests_remaining":490,"requests_reset":"2099-01-01T00:00:00Z","input_tokens_limit":50000,"input_tokens_remaining":45000,"input_tokens_reset":"2099-01-01T00:00:00Z","output_tokens_limit":10000,"output_tokens_remaining":9000,"output_tokens_reset":"2099-01-01T00:00:00Z"}}`

	usages, ok := ParseClaudeRateLimitEvent(raw, "claude-pool", "Claude Default")
	if !ok {
		t.Fatal("expected ok=true for valid rate_limit event")
	}
	if len(usages) != 2 {
		t.Fatalf("expected 2 usage rows, got %d", len(usages))
	}

	req := usages[0]
	if req.Window != "requests" {
		t.Errorf("window = %q, want requests", req.Window)
	}
	if req.LimitValue != 500 || req.Used != 10 {
		t.Errorf("limit=%d used=%d, want 500/10", req.LimitValue, req.Used)
	}

	tok := usages[1]
	if tok.Window != "tokens" {
		t.Errorf("window = %q, want tokens", tok.Window)
	}
	// total = 50000+10000 = 60000; used = (50000-45000)+(10000-9000) = 6000
	if tok.LimitValue != 60000 || tok.Used != 6000 {
		t.Errorf("limit=%d used=%d, want 60000/6000", tok.LimitValue, tok.Used)
	}
}

func TestParseClaudeRateLimitEvent_NotRateLimit(t *testing.T) {
	raw := `{"type":"system","subtype":"init"}`
	_, ok := ParseClaudeRateLimitEvent(raw, "pool", "MyPool")
	if ok {
		t.Error("expected ok=false for non-rate_limit event")
	}
}

func TestParseResetTime_Duration(t *testing.T) {
	now := time.Date(2099, 1, 1, 0, 0, 0, 0, time.UTC)
	got := parseResetTime("6m0s", now)
	want := now.Add(6 * time.Minute)
	if !got.Equal(want) {
		t.Errorf("parseResetTime(6m0s) = %v, want %v", got, want)
	}
}

func TestParseResetTime_RFC3339(t *testing.T) {
	now := time.Now()
	got := parseResetTime("2099-06-01T12:00:00Z", now)
	want, _ := time.Parse(time.RFC3339, "2099-06-01T12:00:00Z")
	if !got.Equal(want) {
		t.Errorf("parseResetTime(RFC3339) = %v, want %v", got, want)
	}
}

func TestParseResetTime_Empty(t *testing.T) {
	now := time.Now()
	got := parseResetTime("", now)
	if got.Before(now.Add(59 * time.Minute)) {
		t.Errorf("empty reset should default to ~1h, got %v", got)
	}
}
