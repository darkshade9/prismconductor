// Package session manages PTY worker subprocesses (PRISMCONDUCTOR_PLAN.md §10).
package session

// Patterns matched in PTY output (§10.3). Plain string contains, no regex.
const (
	PatternQuestion    = "Question: "
	PatternPlanWritten = "Plan written to .prismconductor/plans/"
	PatternComplete    = "Work complete."
	PatternBlocked     = "BLOCKED:"
	PatternPROpened    = "PR_OPENED: "
)
