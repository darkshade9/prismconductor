package session

import (
	"strings"
	"time"

	"prismconductor/internal/types"
)

const ringCap = 5

// activityRing is a fixed-capacity in-memory ring buffer for ActivityEntry.
// Not thread-safe — callers must hold actMu.
type activityRing struct {
	entries [ringCap]types.ActivityEntry
	count   int
	head    int // index where next write goes
}

func (r *activityRing) push(e types.ActivityEntry) {
	r.entries[r.head] = e
	r.head = (r.head + 1) % ringCap
	if r.count < ringCap {
		r.count++
	}
}

// tail returns entries in order from oldest to newest.
func (r *activityRing) tail() []types.ActivityEntry {
	if r.count == 0 {
		return nil
	}
	out := make([]types.ActivityEntry, r.count)
	start := (r.head - r.count + ringCap) % ringCap
	for i := 0; i < r.count; i++ {
		out[i] = r.entries[(start+i)%ringCap]
	}
	return out
}

// parseToolLine splits a "@tool " prefixed line into (toolName, argsRaw).
// argsRaw is the remainder after the tool name; may be empty.
func parseToolLine(line string) (toolName, argsRaw string) {
	rest := strings.TrimPrefix(line, RoleTool)
	if i := strings.Index(rest, " "); i > 0 {
		return rest[:i], rest[i+1:]
	}
	return rest, ""
}

// argsSummary truncates s to ≤50 chars and escapes newlines.
func argsSummary(s string) string {
	s = strings.ReplaceAll(s, "\r\n", "↵")
	s = strings.ReplaceAll(s, "\n", "↵")
	s = strings.ReplaceAll(s, "\r", "")
	if len(s) > 50 {
		return s[:50]
	}
	return s
}

// capArgsJSON caps raw JSON at 2048 bytes for the popover display.
const argsJSONCap = 2048

func capArgsJSON(s string) string {
	if len(s) > argsJSONCap {
		return s[:argsJSONCap]
	}
	return s
}

// newActivityEntry builds an ActivityEntry from a @tool line.
func newActivityEntry(line string) types.ActivityEntry {
	name, args := parseToolLine(line)
	return types.ActivityEntry{
		ToolName:    name,
		ArgsSummary: argsSummary(args),
		ArgsJSON:    capArgsJSON(args),
		At:          time.Now(),
	}
}
