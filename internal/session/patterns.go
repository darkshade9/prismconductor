// Package session manages PTY worker subprocesses (PRISMCONDUCTOR_PLAN.md §10).
package session

// Patterns matched in PTY output (§10.3). Plain string contains, no regex.
const (
	PatternQuestion        = "Question: "
	PatternPlanWritten     = "Plan written to .prismconductor/plans/"
	PatternComplete        = "Work complete."
	PatternBlocked         = "BLOCKED:"
	PatternPROpened        = "PR_OPENED: "
	PatternQuestionPending = "QUESTION_PENDING: "
	// PatternNeedsPR is emitted by the execute skill when it cannot push/sign
	// but the work itself succeeded. The conductor routes these to REVIEW with
	// a "manual push needed" badge instead of a generic BLOCKED state (#157).
	PatternNeedsPR = "NEEDS_PR:"
)

// NeedsPRSignals is the list of push/signing failure substrings that, when
// matched in worker output, indicate a NEEDS_PR condition (#157). Used as a
// belt-and-suspenders fallback for skills that don't yet emit PatternNeedsPR
// explicitly. Plain string contains, no regex (§10.3 CLAUDE.md rule).
var NeedsPRSignals = []string{
	"gpg failed to sign",
	"signing failed",
	"secret key not available",
	"no secret key",
	"Permission denied",
	"Authentication failed",
	"Authentication required",
	"could not read Username",
	"fatal: could not read Password",
	"remote: pre-receive hook declined",
	"protected branch",
	"GH006",
	"signed commits required",
	"required status check",
	"Could not resolve host",
	"Connection refused",
	"! [remote rejected]",
	"error: failed to push some refs",
	"unable to access",
	"gnutls_handshake() failed",
}
