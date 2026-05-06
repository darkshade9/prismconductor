package retry

import (
	"testing"
	"time"

	"prismconductor/internal/types"
)

func TestBackoffWithJitter(t *testing.T) {
	policy := types.RetryPolicy{
		MaxAttempts: 3,
		BaseBackoff: 10 * time.Second,
		MaxBackoff:  5 * time.Minute,
		JitterPct:   0, // no jitter for deterministic tests
	}

	tests := []struct {
		attempt int
		want    time.Duration
	}{
		{1, 10 * time.Second},
		{2, 20 * time.Second},
		{3, 40 * time.Second},
		{4, 80 * time.Second},
	}

	for _, tt := range tests {
		got := backoffWithJitter(tt.attempt, policy)
		if got != tt.want {
			t.Errorf("backoffWithJitter(%d, ...) = %v, want %v", tt.attempt, got, tt.want)
		}
	}
}

func TestBackoffWithJitterCapsAtMax(t *testing.T) {
	policy := types.RetryPolicy{
		MaxAttempts: 10,
		BaseBackoff: 10 * time.Second,
		MaxBackoff:  60 * time.Second,
		JitterPct:   0,
	}
	// attempt 4 would be 80s without cap; must be clamped to 60s
	got := backoffWithJitter(4, policy)
	if got != 60*time.Second {
		t.Errorf("expected max cap 60s, got %v", got)
	}
}

func TestBackoffWithJitterAttemptZeroTreatedAsOne(t *testing.T) {
	policy := types.RetryPolicy{
		MaxAttempts: 3,
		BaseBackoff: 10 * time.Second,
		MaxBackoff:  5 * time.Minute,
		JitterPct:   0,
	}
	got0 := backoffWithJitter(0, policy)
	got1 := backoffWithJitter(1, policy)
	if got0 != got1 {
		t.Errorf("attempt 0 should equal attempt 1: got0=%v got1=%v", got0, got1)
	}
}

func TestBackoffWithJitterAppliesJitter(t *testing.T) {
	policy := types.RetryPolicy{
		MaxAttempts: 3,
		BaseBackoff: 10 * time.Second,
		MaxBackoff:  5 * time.Minute,
		JitterPct:   25,
	}
	base := 10 * time.Second
	minExpected := time.Duration(float64(base) * 0.75)
	maxExpected := time.Duration(float64(base) * 1.25)

	// Run several times to verify the jitter stays in range.
	for i := 0; i < 50; i++ {
		got := backoffWithJitter(1, policy)
		if got < minExpected || got > maxExpected {
			t.Errorf("jittered backoff %v outside [%v, %v]", got, minExpected, maxExpected)
		}
	}
}

func TestIssueKey(t *testing.T) {
	if got := issueKey("ws-1", 42); got != "ws-1#42" {
		t.Errorf("issueKey = %q, want %q", got, "ws-1#42")
	}
}

func TestEffectivePolicyFallsBackToDefault(t *testing.T) {
	ws := types.Workspace{}
	p := effectivePolicy(ws)
	if p.MaxAttempts != types.DefaultRetryPolicy.MaxAttempts {
		t.Errorf("expected default MaxAttempts %d, got %d", types.DefaultRetryPolicy.MaxAttempts, p.MaxAttempts)
	}
}

func TestEffectivePolicyUsesWorkspaceOverride(t *testing.T) {
	custom := types.RetryPolicy{MaxAttempts: 7}
	ws := types.Workspace{RetryPolicy: &custom}
	p := effectivePolicy(ws)
	if p.MaxAttempts != 7 {
		t.Errorf("expected workspace MaxAttempts 7, got %d", p.MaxAttempts)
	}
}
