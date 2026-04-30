package harness

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func newEnv(t *testing.T) *env {
	t.Helper()
	return &env{
		Cwd:    t.TempDir(),
		Budget: DefaultBudget(),
	}
}

func TestToolRead_HappyPath(t *testing.T) {
	e := newEnv(t)
	if err := os.WriteFile(filepath.Join(e.Cwd, "a.txt"), []byte("hello\nworld\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	out, err := toolRead(context.Background(), e, json.RawMessage(`{"file_path":"a.txt"}`))
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if !strings.Contains(out, "hello") || !strings.Contains(out, "world") {
		t.Errorf("missing content: %q", out)
	}
}

func TestToolRead_RejectsPathEscape(t *testing.T) {
	e := newEnv(t)
	_, err := toolRead(context.Background(), e, json.RawMessage(`{"file_path":"../../etc/passwd"}`))
	if err == nil {
		t.Fatal("expected escape rejection")
	}
	if !strings.Contains(err.Error(), "escapes worktree") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestToolWrite_AtomicAndScoped(t *testing.T) {
	e := newEnv(t)
	_, err := toolWrite(context.Background(), e, json.RawMessage(`{"file_path":"sub/dir/x.txt","content":"hi"}`))
	if err != nil {
		t.Fatalf("write: %v", err)
	}
	b, err := os.ReadFile(filepath.Join(e.Cwd, "sub/dir/x.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if string(b) != "hi" {
		t.Errorf("got %q, want hi", string(b))
	}
	if _, err := toolWrite(context.Background(), e, json.RawMessage(`{"file_path":"../bad","content":"x"}`)); err == nil {
		t.Error("expected escape rejection")
	}
}

// TestToolWrite_RejectsBadPlan pins the harness's pre-write validation
// boundary (issue #71): a plan written under .prismconductor/plans/ that
// doesn't satisfy §9.1 must be rejected, no file landing on disk, and the
// error must name the offending field so the model can self-correct on
// the next turn.
func TestToolWrite_RejectsBadPlan(t *testing.T) {
	e := newEnv(t)
	body := `{
		"issue": 5,
		"rev": 1,
		"plan_markdown": "x",
		"files_to_modify": [],
		"dependencies_detected": [],
		"questions": [],
		"estimated_complexity": "S",
		"ready_to_execute": false
	}`
	args, _ := json.Marshal(map[string]string{
		"file_path": ".prismconductor/plans/5-rev1.json",
		"content":   body,
	})
	_, err := toolWrite(context.Background(), e, args)
	if err == nil {
		t.Fatal("expected validator rejection for bad plan, got nil")
	}
	if !strings.Contains(err.Error(), "issue_number") {
		t.Errorf("error should name `issue_number` so the model can fix: %v", err)
	}
	// File must NOT exist on disk.
	if _, statErr := os.Stat(filepath.Join(e.Cwd, ".prismconductor/plans/5-rev1.json")); statErr == nil {
		t.Error("rejected plan should not have been written to disk")
	}
}

// TestToolWrite_AcceptsGoodPlan pins the happy path: a §9.1-conforming plan
// under the plans dir writes successfully through the validator.
func TestToolWrite_AcceptsGoodPlan(t *testing.T) {
	e := newEnv(t)
	body := `{
		"issue_number": 5,
		"revision": 1,
		"plan_markdown": "x",
		"files_to_modify": [],
		"dependencies_detected": [],
		"questions": [],
		"estimated_complexity": "S",
		"ready_to_execute": false
	}`
	args, _ := json.Marshal(map[string]string{
		"file_path": ".prismconductor/plans/5-rev1.json",
		"content":   body,
	})
	out, err := toolWrite(context.Background(), e, args)
	if err != nil {
		t.Fatalf("good plan rejected: %v", err)
	}
	if !strings.Contains(out, "5-rev1.json") {
		t.Errorf("write result should reference the path: %q", out)
	}
	if _, statErr := os.Stat(filepath.Join(e.Cwd, ".prismconductor/plans/5-rev1.json")); statErr != nil {
		t.Errorf("good plan should be on disk: %v", statErr)
	}
}

// TestToolWrite_NonPlanPathBypassesValidator pins that the validator is
// scoped to plan-file paths only — regular code edits under the worktree
// are not subject to §9.1.
func TestToolWrite_NonPlanPathBypassesValidator(t *testing.T) {
	e := newEnv(t)
	args, _ := json.Marshal(map[string]string{
		"file_path": "src/main.go",
		"content":   `package main // not a plan file`,
	})
	if _, err := toolWrite(context.Background(), e, args); err != nil {
		t.Fatalf("non-plan write should bypass validator: %v", err)
	}
}

func TestToolEdit_NonUniqueOldStringRejected(t *testing.T) {
	e := newEnv(t)
	if err := os.WriteFile(filepath.Join(e.Cwd, "f.txt"), []byte("foo bar foo"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := toolEdit(context.Background(), e, json.RawMessage(`{"file_path":"f.txt","old_string":"foo","new_string":"baz"}`))
	if err == nil {
		t.Fatal("expected non-unique error")
	}
	if !strings.Contains(err.Error(), "matches 2 times") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestToolEdit_ReplaceAll(t *testing.T) {
	e := newEnv(t)
	if err := os.WriteFile(filepath.Join(e.Cwd, "f.txt"), []byte("foo bar foo"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := toolEdit(context.Background(), e, json.RawMessage(`{"file_path":"f.txt","old_string":"foo","new_string":"baz","replace_all":true}`))
	if err != nil {
		t.Fatalf("edit: %v", err)
	}
	b, _ := os.ReadFile(filepath.Join(e.Cwd, "f.txt"))
	if string(b) != "baz bar baz" {
		t.Errorf("got %q, want %q", string(b), "baz bar baz")
	}
}

func TestToolBash_RunsInCwdAndCapturesOutput(t *testing.T) {
	e := newEnv(t)
	out, err := toolBash(context.Background(), e, json.RawMessage(`{"command":"echo hello && pwd"}`))
	if err != nil {
		t.Fatalf("bash: %v", err)
	}
	if !strings.Contains(out, "hello") {
		t.Errorf("missing stdout: %s", out)
	}
	if !strings.Contains(out, e.Cwd) {
		t.Errorf("bash didn't run in cwd %s: %s", e.Cwd, out)
	}
}

func TestToolBash_TimeoutKills(t *testing.T) {
	e := newEnv(t)
	e.Budget.BashTimeout = 200 * time.Millisecond
	out, err := toolBash(context.Background(), e, json.RawMessage(`{"command":"sleep 5"}`))
	if err != nil {
		t.Fatalf("bash: %v", err)
	}
	if !strings.Contains(out, "timed out") {
		t.Errorf("expected timed-out marker, got: %s", out)
	}
}

func TestToolBash_OutputCapTrims(t *testing.T) {
	e := newEnv(t)
	e.Budget.OutputCap = 64
	out, err := toolBash(context.Background(), e, json.RawMessage(`{"command":"yes hello | head -c 1024"}`))
	if err != nil {
		t.Fatalf("bash: %v", err)
	}
	if !strings.Contains(out, "output truncated") {
		t.Errorf("expected truncation marker, got: %s", out)
	}
}

func TestToolGlob_FindsAndSortsByMtime(t *testing.T) {
	e := newEnv(t)
	older := filepath.Join(e.Cwd, "old.go")
	newer := filepath.Join(e.Cwd, "new.go")
	if err := os.WriteFile(older, []byte("// old"), 0o644); err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	if err := os.Chtimes(older, now.Add(-2*time.Hour), now.Add(-2*time.Hour)); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(newer, []byte("// new"), 0o644); err != nil {
		t.Fatal(err)
	}
	out, err := toolGlob(context.Background(), e, json.RawMessage(`{"pattern":"*.go"}`))
	if err != nil {
		t.Fatalf("glob: %v", err)
	}
	idxNew := strings.Index(out, "new.go")
	idxOld := strings.Index(out, "old.go")
	if idxNew < 0 || idxOld < 0 {
		t.Fatalf("missing entries: %s", out)
	}
	if idxNew > idxOld {
		t.Errorf("expected newest-first, got: %s", out)
	}
}

func TestToolGrep_FindsLineWithFilePrefix(t *testing.T) {
	e := newEnv(t)
	if err := os.WriteFile(filepath.Join(e.Cwd, "a.go"), []byte("hello\nfoo bar\nbaz"), 0o644); err != nil {
		t.Fatal(err)
	}
	out, err := toolGrep(context.Background(), e, json.RawMessage(`{"pattern":"foo"}`))
	if err != nil {
		t.Fatalf("grep: %v", err)
	}
	if !strings.Contains(out, "a.go:2:foo bar") {
		t.Errorf("expected file:line:content prefix, got: %s", out)
	}
}

func TestToolTodoWrite_ReplacesAndReportsCount(t *testing.T) {
	e := newEnv(t)
	out, err := toolTodoWrite(context.Background(), e, json.RawMessage(`{"todos":[{"content":"step one","status":"pending","activeForm":"doing one"}]}`))
	if err != nil {
		t.Fatalf("todo: %v", err)
	}
	if !strings.Contains(out, "1 items") {
		t.Errorf("missing count: %s", out)
	}
	if len(e.todos) != 1 || e.todos[0].Content != "step one" {
		t.Errorf("env todos not updated: %+v", e.todos)
	}
}
