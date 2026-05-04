package git

import (
	"reflect"
	"strings"
	"testing"
)

func TestParseWorktreeList(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want []Entry
	}{
		{
			name: "empty",
			in:   "",
			want: nil,
		},
		{
			name: "single worktree",
			in: "worktree /repo\n" +
				"HEAD abc123\n" +
				"branch refs/heads/main\n",
			want: []Entry{
				{Path: "/repo", HEAD: "abc123", Branch: "refs/heads/main"},
			},
		},
		{
			name: "two worktrees with trailing whitespace",
			in: "worktree /repo\n" +
				"HEAD abc123\n" +
				"branch refs/heads/main\n" +
				"\n" +
				"worktree /repo/.prismconductor/worktrees/ws-7\n" +
				"HEAD def456\n" +
				"branch refs/heads/feat/issue-7-foo\n" +
				"\n",
			want: []Entry{
				{Path: "/repo", HEAD: "abc123", Branch: "refs/heads/main"},
				{Path: "/repo/.prismconductor/worktrees/ws-7", HEAD: "def456", Branch: "refs/heads/feat/issue-7-foo"},
			},
		},
		{
			name: "detached head has no branch line",
			in: "worktree /repo\n" +
				"HEAD abc123\n" +
				"detached\n",
			want: []Entry{
				{Path: "/repo", HEAD: "abc123"},
			},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := parseWorktreeList(tc.in)
			if !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("parseWorktreeList\n got: %#v\nwant: %#v", got, tc.want)
			}
		})
	}
}

// TestMergeConflictFiles_RealRepo runs against a fixture repo built fresh in
// a temp dir. Two branches diverge with a conflicting edit on one file and
// a non-conflicting addition on another. MergeConflictFiles must return
// only the conflicting file — NOT the additional file the head branch
// touched. Pre-fix the function returned the merge-tree SHA as a "file"
// AND treated git's exit-1 (conflicts present) as an error → caller fell
// back to the GitHub PR-files API which lists every modified file.
// Witnessed live on PR #181 (1 actual conflict, 11 reported).
func TestMergeConflictFiles_RealRepo(t *testing.T) {
	dir := t.TempDir()
	mustRun := func(args ...string) {
		t.Helper()
		if _, err := run(dir, args[0], args[1:]...); err != nil {
			t.Fatalf("%v: %v", args, err)
		}
	}
	write := func(path, body string) {
		t.Helper()
		if _, err := run(dir, "sh", "-c", "printf %s "+shellQuote(body)+" > "+shellQuote(path)); err != nil {
			t.Fatalf("write %s: %v", path, err)
		}
	}

	// Init repo, configure user (required for commit).
	mustRun("git", "init", "-q", "-b", "main")
	mustRun("git", "config", "user.email", "test@example.com")
	mustRun("git", "config", "user.name", "Test")

	// Initial commit on main.
	write("a.txt", "main version 1\n")
	write("b.txt", "shared\n")
	mustRun("git", "add", ".")
	mustRun("git", "commit", "-q", "-m", "initial")

	// Branch off, edit a.txt and add c.txt.
	mustRun("git", "checkout", "-q", "-b", "feature")
	write("a.txt", "feature edit\n")
	write("c.txt", "added by feature\n")
	mustRun("git", "add", ".")
	mustRun("git", "commit", "-q", "-m", "feature changes")

	// Back to main, edit a.txt differently → conflicts on a.txt only.
	mustRun("git", "checkout", "-q", "main")
	write("a.txt", "main edit\n")
	mustRun("git", "commit", "-aq", "-m", "main changes")

	got, err := MergeConflictFiles(dir, "main", "feature")
	if err != nil {
		t.Fatalf("MergeConflictFiles: %v", err)
	}
	want := []string{"a.txt"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("MergeConflictFiles = %v, want %v (must include only the conflicting path, NOT the merge-tree SHA, NOT non-conflicting changes like c.txt)", got, want)
	}
}

// shellQuote wraps a string for safe sh -c interpolation.
func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", "'\\''") + "'"
}
