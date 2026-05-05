package session

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

// makeTranscript writes a JSONL transcript file with the given assistant
// tool-use blocks and returns its path.
func makeTranscript(t *testing.T, lines []string) string {
	t.Helper()
	f, err := os.CreateTemp(t.TempDir(), "transcript-*.log")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	for _, l := range lines {
		fmt.Fprintln(f, l)
	}
	return f.Name()
}

// assistantBash returns a stream-json assistant line with a Bash tool call.
func assistantBash(cmd string) string {
	input, _ := json.Marshal(map[string]string{"command": cmd})
	return buildAssistantLine("Bash", input)
}

// assistantRead returns a stream-json assistant line with a Read tool call.
func assistantRead(filePath string) string {
	input, _ := json.Marshal(map[string]string{"file_path": filePath})
	return buildAssistantLine("Read", input)
}

// assistantWebFetch returns a stream-json assistant line with a WebFetch tool call.
func assistantWebFetch(url string) string {
	input, _ := json.Marshal(map[string]string{"url": url})
	return buildAssistantLine("WebFetch", input)
}

func buildAssistantLine(name string, input json.RawMessage) string {
	msg := map[string]any{
		"type": "assistant",
		"message": map[string]any{
			"content": []map[string]any{
				{
					"type":  "tool_use",
					"id":    "tu_1",
					"name":  name,
					"input": json.RawMessage(input),
				},
			},
		},
	}
	b, _ := json.Marshal(msg)
	return string(b)
}

// --- ScanForIssueRead ---

func TestScanForIssueRead_DetectsGhIssueView(t *testing.T) {
	path := makeTranscript(t, []string{
		`{"type":"system","subtype":"init"}`,
		assistantBash("gh issue view 42 --json body"),
	})
	ok, err := ScanForIssueRead(path, 42)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !ok {
		t.Fatal("expected true (gh issue view 42 present), got false")
	}
}

func TestScanForIssueRead_RejectsWrongIssueNumber(t *testing.T) {
	path := makeTranscript(t, []string{
		assistantBash("gh issue view 99 --json body"),
	})
	ok, err := ScanForIssueRead(path, 42)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ok {
		t.Fatal("expected false (different issue number), got true")
	}
}

func TestScanForIssueRead_DetectsPayloadFileRead(t *testing.T) {
	path := makeTranscript(t, []string{
		assistantRead(".prismconductor/issue-42.md"),
	})
	ok, err := ScanForIssueRead(path, 42)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !ok {
		t.Fatal("expected true (pre-fetched payload read), got false")
	}
}

func TestScanForIssueRead_DetectsAbsolutePayloadPath(t *testing.T) {
	repoPath := t.TempDir()
	payloadPath := IssuePayloadPath(repoPath, 42)
	path := makeTranscript(t, []string{
		assistantRead(payloadPath),
	})
	ok, err := ScanForIssueRead(path, 42)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !ok {
		t.Fatal("expected true (absolute payload path read), got false")
	}
}

func TestScanForIssueRead_DetectsWebFetch(t *testing.T) {
	path := makeTranscript(t, []string{
		assistantWebFetch("https://api.github.com/repos/owner/repo/issues/42"),
	})
	ok, err := ScanForIssueRead(path, 42)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !ok {
		t.Fatal("expected true (WebFetch /issues/42), got false")
	}
}

func TestScanForIssueRead_NoEvidenceReturnsFalse(t *testing.T) {
	path := makeTranscript(t, []string{
		`{"type":"system","subtype":"init"}`,
		assistantBash("ls -la"),
		assistantRead("README.md"),
		assistantBash("go build ./..."),
	})
	ok, err := ScanForIssueRead(path, 42)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ok {
		t.Fatal("expected false (no issue-read evidence), got true")
	}
}

func TestScanForIssueRead_EmptyTranscript(t *testing.T) {
	path := makeTranscript(t, nil)
	ok, err := ScanForIssueRead(path, 42)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ok {
		t.Fatal("expected false for empty transcript, got true")
	}
}

func TestScanForIssueRead_NonJSONLinesIgnored(t *testing.T) {
	path := makeTranscript(t, []string{
		"starting session...",
		"model: claude-opus-4",
		assistantBash("gh issue view 42"),
	})
	ok, err := ScanForIssueRead(path, 42)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !ok {
		t.Fatal("expected true despite non-JSON prefix lines, got false")
	}
}

func TestScanForIssueRead_MissingFileReturnsError(t *testing.T) {
	_, err := ScanForIssueRead(filepath.Join(t.TempDir(), "noexist.log"), 42)
	if err == nil {
		t.Fatal("expected error for missing file, got nil")
	}
}

// --- IssuePayloadPath ---

func TestIssuePayloadPath(t *testing.T) {
	got := IssuePayloadPath("/repo", 197)
	want := "/repo/.prismconductor/issue-197.md"
	if got != want {
		t.Fatalf("IssuePayloadPath = %q, want %q", got, want)
	}
}
