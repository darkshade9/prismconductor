package session

import "strings"

// Quota error substrings emitted by the codex CLI when a ChatGPT subscription
// limit is reached. Plain string contains, no regex (§10.3).
var codexQuotaSignals = []string{
	"exceeded your subscription quota",
	"subscription limit reached",
	"ChatGPT subscription",
	"rate limit exceeded",
	"quota exceeded",
	"usage limit",
	"usage cap",
	"hit your limit",
}

// codexResetSignals are prefixes/substrings in codex quota error output that
// precede a reset time. They're used to extract the human-readable reset time
// from the raw reason string.
var codexResetSignals = []string{
	"resets at",
	"resets on",
	"reset at",
	"try again after",
	"available again at",
}

// ParseCodexQuotaError inspects a BLOCKED reason string and reports whether
// it represents a codex subscription-quota failure. When it does, resetAt is
// the reset-time substring extracted from the reason (empty when not found).
func ParseCodexQuotaError(reason string) (isQuota bool, resetAt string) {
	lower := strings.ToLower(reason)
	for _, sig := range codexQuotaSignals {
		if strings.Contains(lower, sig) {
			isQuota = true
			break
		}
	}
	if !isQuota {
		return false, ""
	}
	for _, sig := range codexResetSignals {
		if i := strings.Index(lower, sig); i >= 0 {
			// Return everything after the reset signal as the time hint.
			suffix := strings.TrimSpace(reason[i+len(sig):])
			// Trim trailing punctuation.
			suffix = strings.TrimRight(suffix, ".;,")
			if suffix != "" {
				return true, suffix
			}
		}
	}
	return true, ""
}
