package session

import (
	"strings"
	"testing"

	"prismconductor/internal/types"
)

// TestContinueExecutePrompt_BundledMode pins the wire shape that
// SpawnContinue hands the worker: the standard execute argv plus
// `--continue --note '<quoted>'`. The bundled-mode path is the load-bearing
// one — Hybrid/Native are forwarded the same suffix and downstream tools
// must respect it.
func TestContinueExecutePrompt_BundledMode(t *testing.T) {
	ws := types.Workspace{
		ID:           "ws1",
		RepoPath:     "/repo",
		SkillProfile: types.SkillProfile{Mode: types.SkillModeBundled},
	}
	issue := types.Issue{Number: 42, Title: "fix the thing"}
	plan := types.Plan{Revision: 3}
	got := continueExecutePrompt(ws, issue, plan, "tests in TestFoo are failing")

	for _, want := range []string{
		"/conductor-execute",
		"--issue 42",
		"--repo /repo",
		"--revision 3",
		"--continue",
		"--note ",
		"'tests in TestFoo are failing'",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("prompt missing %q\n  got: %s", want, got)
		}
	}
}

// TestContinueExecutePrompt_QuotesNote pins the shell-quoting contract: a
// note containing a single quote must round-trip through to the worker
// without breaking the outer single-quoted string. POSIX `'\''` escape.
func TestContinueExecutePrompt_QuotesNote(t *testing.T) {
	ws := types.Workspace{ID: "ws", RepoPath: "/r", SkillProfile: types.SkillProfile{Mode: types.SkillModeBundled}}
	issue := types.Issue{Number: 1}
	plan := types.Plan{Revision: 1}
	got := continueExecutePrompt(ws, issue, plan, "it's broken")
	want := "'it'\\''s broken'"
	if !strings.Contains(got, want) {
		t.Errorf("quote handling: want %q in prompt, got: %s", want, got)
	}
}

func TestShellQuote_Empty(t *testing.T) {
	if got, want := shellQuote(""), "''"; got != want {
		t.Errorf("shellQuote(\"\") = %q, want %q", got, want)
	}
}

func TestShellQuote_NoQuotes(t *testing.T) {
	if got, want := shellQuote("hello world"), "'hello world'"; got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}
