package cardstate

import (
	"strings"

	"prismconductor/internal/types"
)

// retryableExitCodes are subprocess exit codes that signal transient termination:
//
//	124 = timeout (GNU timeout / ulimit)
//	130 = SIGINT received by subprocess
//	137 = SIGKILL (OOM or external kill)
//	143 = SIGTERM (graceful shutdown)
var retryableExitCodes = map[int]bool{124: true, 130: true, 137: true, 143: true}

// retryableHarnessKeywords are lower-case substrings in a "blocked" reason that
// indicate a transient network / rate-limit condition rather than a hard failure.
var retryableHarnessKeywords = []string{
	"network",
	"timeout",
	"connection refused",
	"503",
	"5xx",
	"rate limit",
	"temporary",
	"i/o error",
}

// IsRetryable returns true when cause represents a transient condition worth
// automatically retrying. Deterministic failures (lint errors, malformed plans,
// explicit user cancellations, hard BLOCKED: sentinels) return false.
func IsRetryable(cause *types.FailureCause) bool {
	if cause == nil {
		return false
	}
	switch cause.Kind {
	case "exit_code":
		// ExitCode == 0 means the field was not populated (legacy path before
		// manager.go was updated to stamp ExitCode on FailureCause). Treat as
		// "exit code missing" → retryable per the plan spec.
		if cause.ExitCode == 0 {
			return true
		}
		return retryableExitCodes[cause.ExitCode]
	case "signal":
		// Process killed by OS (SIGKILL) or graceful shutdown (SIGTERM) are
		// transient. SIGINT usually means the user cancelled — but the signal
		// string may be "signal: interrupt" which we conservatively allow since
		// the cancellation could be harness-side, not user-initiated.
		return strings.Contains(cause.Signal, "killed") ||
			strings.Contains(cause.Signal, "terminated") ||
			strings.Contains(cause.Signal, "interrupt")
	case "blocked":
		// BLOCKED: from the worker is a hard failure in the general case
		// (lint error, malformed plan, etc.). Only retry when the reason text
		// contains clear network / transient indicators.
		r := strings.ToLower(cause.Reason)
		for _, kw := range retryableHarnessKeywords {
			if strings.Contains(r, kw) {
				return true
			}
		}
		return false
	case types.FailureKindQuotaExceeded:
		// Provider quota exhaustion is always retryable; the scheduler uses
		// RetryAfter to wait until reset time rather than exponential backoff.
		return true
	}
	return false
}
