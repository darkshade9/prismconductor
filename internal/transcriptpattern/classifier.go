// Package transcriptpattern is the Phase 7 pattern detector (PRISMCONDUCTOR_PLAN.md §15.11). Stub.
package transcriptpattern

// Suggestion is what the Skill Studio surfaces to the user.
type Suggestion struct {
	Kind        string   // "extract_rule" | "extract_skill" | "bootstrap_claudemd"
	Reason      string   // human-readable
	TranscriptIDs []string
}

// Detect scans transcripts and returns extraction suggestions. Stub: empty.
func Detect(_ []string) []Suggestion { return nil }
