package session

import (
	"strings"
	"testing"
	"time"

	"prismconductor/internal/types"
)

func entry(tool, args string) types.ActivityEntry {
	return types.ActivityEntry{ToolName: tool, ArgsSummary: args, At: time.Now()}
}

func TestRingTailEmpty(t *testing.T) {
	var r activityRing
	if got := r.tail(); got != nil {
		t.Errorf("empty ring: want nil tail, got %v", got)
	}
}

func TestRingTailUnderCap(t *testing.T) {
	var r activityRing
	for i := 0; i < 3; i++ {
		r.push(entry("Tool", "arg"))
	}
	if len(r.tail()) != 3 {
		t.Fatalf("want 3 entries, got %d", len(r.tail()))
	}
}

func TestRingTailAtCap(t *testing.T) {
	var r activityRing
	for i := 0; i < ringCap; i++ {
		r.push(entry("T", string(rune('a'+i))))
	}
	if len(r.tail()) != ringCap {
		t.Fatalf("want %d entries, got %d", ringCap, len(r.tail()))
	}
}

func TestRingOverflowEvictsOldest(t *testing.T) {
	var r activityRing
	// Push ringCap+2 entries; only the last ringCap should survive.
	for i := 0; i < ringCap+2; i++ {
		r.push(entry("Tool", string(rune('a'+i))))
	}
	tail := r.tail()
	if len(tail) != ringCap {
		t.Fatalf("want %d entries after overflow, got %d", ringCap, len(tail))
	}
	// Entry at index 0 is the oldest surviving one (index 2 → 'c').
	wantFirst := string(rune('a' + 2))
	if tail[0].ArgsSummary != wantFirst {
		t.Errorf("oldest entry: want %q, got %q", wantFirst, tail[0].ArgsSummary)
	}
	wantLast := string(rune('a' + ringCap + 1))
	if tail[ringCap-1].ArgsSummary != wantLast {
		t.Errorf("newest entry: want %q, got %q", wantLast, tail[ringCap-1].ArgsSummary)
	}
}

func TestRingTailOrderOldestFirst(t *testing.T) {
	var r activityRing
	r.push(entry("A", "1"))
	r.push(entry("B", "2"))
	r.push(entry("C", "3"))
	tail := r.tail()
	if tail[0].ToolName != "A" || tail[2].ToolName != "C" {
		t.Errorf("tail order wrong: %v", tail)
	}
}

func TestArgsSummaryTruncates(t *testing.T) {
	s := strings.Repeat("x", 100)
	got := argsSummary(s)
	if len(got) > 50 {
		t.Errorf("want ≤50 chars, got %d", len(got))
	}
}

func TestArgsSummaryEscapesNewlines(t *testing.T) {
	got := argsSummary("a\nb\r\nc")
	if strings.ContainsAny(got, "\n\r") {
		t.Errorf("newlines not escaped: %q", got)
	}
}

func TestCapArgsJSON(t *testing.T) {
	big := strings.Repeat("x", argsJSONCap+100)
	got := capArgsJSON(big)
	if len(got) != argsJSONCap {
		t.Errorf("want %d, got %d", argsJSONCap, len(got))
	}

	small := "hello"
	if capArgsJSON(small) != small {
		t.Errorf("capArgsJSON changed small string")
	}
}

func TestParseToolLineNoArgs(t *testing.T) {
	name, args := parseToolLine(RoleTool + "Bash")
	if name != "Bash" || args != "" {
		t.Errorf("got name=%q args=%q", name, args)
	}
}

func TestParseToolLineWithArgs(t *testing.T) {
	name, args := parseToolLine(RoleTool + `Bash {"command":"ls"}`)
	if name != "Bash" {
		t.Errorf("name: want Bash, got %q", name)
	}
	if args != `{"command":"ls"}` {
		t.Errorf("args: got %q", args)
	}
}

func TestNewActivityEntryExtractsFields(t *testing.T) {
	line := RoleTool + `Read {"file_path":"/tmp/x"}`
	e := newActivityEntry(line)
	if e.ToolName != "Read" {
		t.Errorf("ToolName: want Read, got %q", e.ToolName)
	}
	if e.ArgsJSON != `{"file_path":"/tmp/x"}` {
		t.Errorf("ArgsJSON: got %q", e.ArgsJSON)
	}
	if e.ArgsSummary == "" {
		t.Error("ArgsSummary: want non-empty")
	}
	if e.At.IsZero() {
		t.Error("At: want non-zero time")
	}
}
