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

// DefaultBudget mirrors the defaults documented in §10.5: 60 turns, 200k
// cumulative input tokens, 5-minute Bash timeout, 50 KB Bash output cap.
func DefaultBudget() Budget {
	return Budget{
		MaxTurns:       60,
		MaxInputTokens: 200_000,
		BashTimeout:    5 * time.Minute,
		OutputCap:      50 * 1024,
	}
}

type budgetState struct {
	turns       int
	inputTokens int
}

func (b *budgetState) tickTurn()           { b.turns++ }
func (b *budgetState) addUsage(in, _ int)  { b.inputTokens += in }
func (b budgetState) Turns() int           { return b.turns }
func (b budgetState) InputTokens() int     { return b.inputTokens }
