package session

import (
	"testing"
	"time"

	"prismconductor/internal/harness"
	"prismconductor/internal/types"
)

func TestPoolBudgetDefaults(t *testing.T) {
	def := harness.DefaultBudget()
	got := poolBudget(types.Pool{})
	if got.MaxTurns != def.MaxTurns {
		t.Errorf("MaxTurns: got %d, want %d", got.MaxTurns, def.MaxTurns)
	}
	if got.MaxInputTokens != def.MaxInputTokens {
		t.Errorf("MaxInputTokens: got %d, want %d", got.MaxInputTokens, def.MaxInputTokens)
	}
	if got.BashTimeout != def.BashTimeout {
		t.Errorf("BashTimeout: got %v, want %v", got.BashTimeout, def.BashTimeout)
	}
	if got.OutputCap != def.OutputCap {
		t.Errorf("OutputCap: got %d, want %d", got.OutputCap, def.OutputCap)
	}
}

func TestPoolBudgetOverridesApplied(t *testing.T) {
	turns := 200
	tokens := 5_000_000
	timeout := 10 * time.Minute
	cap := 100 * 1024
	p := types.Pool{
		MaxTurns:       &turns,
		MaxInputTokens: &tokens,
		BashTimeout:    &timeout,
		OutputCap:      &cap,
	}
	got := poolBudget(p)
	if got.MaxTurns != turns {
		t.Errorf("MaxTurns: got %d, want %d", got.MaxTurns, turns)
	}
	if got.MaxInputTokens != tokens {
		t.Errorf("MaxInputTokens: got %d, want %d", got.MaxInputTokens, tokens)
	}
	if got.BashTimeout != timeout {
		t.Errorf("BashTimeout: got %v, want %v", got.BashTimeout, timeout)
	}
	if got.OutputCap != cap {
		t.Errorf("OutputCap: got %d, want %d", got.OutputCap, cap)
	}
}

func TestPoolBudgetPartialOverride(t *testing.T) {
	def := harness.DefaultBudget()
	tokens := 5_000_000
	p := types.Pool{MaxInputTokens: &tokens}
	got := poolBudget(p)
	if got.MaxInputTokens != tokens {
		t.Errorf("MaxInputTokens: got %d, want %d", got.MaxInputTokens, tokens)
	}
	// Other fields must remain at default.
	if got.MaxTurns != def.MaxTurns {
		t.Errorf("MaxTurns: got %d, want %d", got.MaxTurns, def.MaxTurns)
	}
	if got.BashTimeout != def.BashTimeout {
		t.Errorf("BashTimeout: got %v, want %v", got.BashTimeout, def.BashTimeout)
	}
	if got.OutputCap != def.OutputCap {
		t.Errorf("OutputCap: got %d, want %d", got.OutputCap, def.OutputCap)
	}
}
