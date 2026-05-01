package harness

import "time"

// Budget caps per-session resource use so a runaway model can't burn cycles
// or wedge the conductor (PRISMCONDUCTOR_PLAN.md §10.5, issue #58 q-budget).
type Budget struct {
	MaxTurns       int           // hard turn cap
	MaxInputTokens int           // cumulative prompt tokens across all turns
	BashTimeout    time.Duration // per-Bash-call default deadline
	OutputCap      int           // bytes captured per Bash call
}

// DefaultBudget caps a single harness session. Bumped from the original
// §10.5 numbers (60 turns / 200K tokens) because real plan runs against
// gpt-5-mini and Gemini regularly re-read files and accumulate cumulative
// input tokens fast — 200K was tripping mid-plan for legitimate, non-runaway
// runs. The new envelope (120 turns / 1M cumulative input tokens) is still
// a hard wall against truly stuck loops, just a less aggressive one.
func DefaultBudget() Budget {
	return Budget{
		MaxTurns:       120,
		MaxInputTokens: 1_000_000,
		BashTimeout:    5 * time.Minute,
		OutputCap:      50 * 1024,
	}
}

type budgetState struct {
	turns        int
	inputTokens  int
	outputTokens int
}

func (b *budgetState) tickTurn()                 { b.turns++ }
func (b *budgetState) addUsage(in, out int)      { b.inputTokens += in; b.outputTokens += out }
func (b budgetState) Turns() int                 { return b.turns }
func (b budgetState) InputTokens() int           { return b.inputTokens }
func (b budgetState) OutputTokens() int          { return b.outputTokens }
