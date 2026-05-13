package cardstate

import (
	"testing"

	"prismconductor/internal/types"
)

func TestIsRetryable(t *testing.T) {
	tests := []struct {
		name  string
		cause *types.FailureCause
		want  bool
	}{
		{"nil cause", nil, false},
		{"unknown kind", &types.FailureCause{Kind: "unknown"}, false},

		// exit_code cases
		{"exit_code zero (legacy missing)", &types.FailureCause{Kind: "exit_code", ExitCode: 0}, true},
		{"exit_code 124 timeout", &types.FailureCause{Kind: "exit_code", ExitCode: 124}, true},
		{"exit_code 130 SIGINT", &types.FailureCause{Kind: "exit_code", ExitCode: 130}, true},
		{"exit_code 137 SIGKILL", &types.FailureCause{Kind: "exit_code", ExitCode: 137}, true},
		{"exit_code 143 SIGTERM", &types.FailureCause{Kind: "exit_code", ExitCode: 143}, true},
		{"exit_code 1 generic error", &types.FailureCause{Kind: "exit_code", ExitCode: 1}, false},
		{"exit_code 2 usage error", &types.FailureCause{Kind: "exit_code", ExitCode: 2}, false},

		// signal cases
		{"signal killed", &types.FailureCause{Kind: "signal", Signal: "signal: killed"}, true},
		{"signal terminated", &types.FailureCause{Kind: "signal", Signal: "signal: terminated"}, true},
		{"signal interrupt", &types.FailureCause{Kind: "signal", Signal: "signal: interrupt"}, true},
		{"signal SIGBUS hard failure", &types.FailureCause{Kind: "signal", Signal: "signal: bus error"}, false},

		// blocked cases
		{"blocked network", &types.FailureCause{Kind: "blocked", Reason: "network unreachable"}, true},
		{"blocked timeout", &types.FailureCause{Kind: "blocked", Reason: "operation timeout"}, true},
		{"blocked connection refused", &types.FailureCause{Kind: "blocked", Reason: "connection refused by host"}, true},
		{"blocked 503", &types.FailureCause{Kind: "blocked", Reason: "upstream returned 503"}, true},
		{"blocked 5xx", &types.FailureCause{Kind: "blocked", Reason: "server error 5xx"}, true},
		{"blocked rate limit", &types.FailureCause{Kind: "blocked", Reason: "rate limit exceeded"}, true},
		{"blocked temporary", &types.FailureCause{Kind: "blocked", Reason: "temporary unavailable"}, true},
		{"blocked i/o error", &types.FailureCause{Kind: "blocked", Reason: "i/o error reading file"}, true},
		{"blocked keyword case insensitive", &types.FailureCause{Kind: "blocked", Reason: "Network Error"}, true},
		{"blocked lint error", &types.FailureCause{Kind: "blocked", Reason: "lint failed — go vet exited 1"}, false},
		{"blocked malformed plan", &types.FailureCause{Kind: "blocked", Reason: "malformed plan JSON"}, false},

		// quota_exceeded cases
		{"quota_exceeded always retryable", &types.FailureCause{Kind: types.FailureKindQuotaExceeded}, true},
		{"quota_exceeded with reason", &types.FailureCause{Kind: types.FailureKindQuotaExceeded, Reason: "gemini free-tier quota exhausted"}, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := IsRetryable(tt.cause)
			if got != tt.want {
				t.Errorf("IsRetryable(%+v) = %v, want %v", tt.cause, got, tt.want)
			}
		})
	}
}
