package session

import "testing"

func TestParseCodexQuotaError(t *testing.T) {
	cases := []struct {
		reason      string
		wantQuota   bool
		wantResetAt string
	}{
		{
			reason:    "tests failed — 3 failures",
			wantQuota: false,
		},
		{
			reason:      "exceeded your subscription quota; resets at 2026-05-14 08:00 UTC",
			wantQuota:   true,
			wantResetAt: "2026-05-14 08:00 UTC",
		},
		{
			reason:      "ChatGPT subscription limit reached — try again after midnight",
			wantQuota:   true,
			wantResetAt: "midnight",
		},
		{
			reason:      "subscription limit reached",
			wantQuota:   true,
			wantResetAt: "",
		},
		{
			reason:      "rate limit exceeded; resets on 2026-05-15",
			wantQuota:   true,
			wantResetAt: "2026-05-15",
		},
		{
			reason:      "quota exceeded",
			wantQuota:   true,
			wantResetAt: "",
		},
		{
			reason:      "hit your limit; available again at 06:00",
			wantQuota:   true,
			wantResetAt: "06:00",
		},
		{
			reason:    "BLOCKED: lint failed — go vet exited 1",
			wantQuota: false,
		},
		{
			reason:    "",
			wantQuota: false,
		},
	}

	for _, tc := range cases {
		got, resetAt := ParseCodexQuotaError(tc.reason)
		if got != tc.wantQuota {
			t.Errorf("ParseCodexQuotaError(%q) isQuota = %v, want %v", tc.reason, got, tc.wantQuota)
		}
		if resetAt != tc.wantResetAt {
			t.Errorf("ParseCodexQuotaError(%q) resetAt = %q, want %q", tc.reason, resetAt, tc.wantResetAt)
		}
	}
}
