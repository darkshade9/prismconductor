// Package git wraps the small set of `git worktree` operations the session
// manager needs to spawn and reap per-execute worktrees (issue #22).
package git

import (
	"fmt"
	"os/exec"
	"strings"
)

// Add fetches origin and creates a worktree at dir on a fresh branch off
// origin/<base>. Uses `-B` so a leftover branch from a prior failed run is
// reset rather than reported as a conflict.
func Add(repoPath, branch, dir, base string) error {
	if _, err := run(repoPath, "git", "fetch", "origin"); err != nil {
		return fmt.Errorf("fetch origin: %w", err)
	}
	out, err := run(repoPath, "git", "worktree", "add", "-B", branch, dir, "origin/"+base)
	if err != nil {
		return fmt.Errorf("worktree add %s -> %s: %w (output: %s)", branch, dir, err, out)
	}
	return nil
}

// Remove tears down a worktree directory and deregisters it. Force-flag is
// always set: the conductor never asks for graceful removal because the worker
// process owns the tree and the conductor's only opportunity to clean is post-
// session.
func Remove(repoPath, dir string) error {
	out, err := run(repoPath, "git", "worktree", "remove", "--force", dir)
	if err != nil {
		return fmt.Errorf("worktree remove %s: %w (output: %s)", dir, err, out)
	}
	return nil
}

// Prune deregisters worktrees whose directories no longer exist on disk.
func Prune(repoPath string) error {
	_, err := run(repoPath, "git", "worktree", "prune")
	return err
}

// Entry is one line of `git worktree list --porcelain` output.
type Entry struct {
	Path   string
	Branch string
	HEAD   string
}

// List returns the registered worktrees for repoPath, including the primary
// checkout. Caller filters by path prefix.
func List(repoPath string) ([]Entry, error) {
	out, err := run(repoPath, "git", "worktree", "list", "--porcelain")
	if err != nil {
		return nil, err
	}
	return parseWorktreeList(out), nil
}

// WorktreeIsEmpty reports whether the worktree has NO user-paid-for work
// in it: no uncommitted changes, no untracked files, and no commits
// ahead of HEAD's upstream-tracked base.
//
// Used by the terminal-state cleanup path to decide whether it's safe to
// remove the worktree on Blocked/Failed exits. The historical policy of
// always removing on Blocked/Failed was a data-loss bug — a worker that
// finished its edits but failed at `git commit -S` (GPG passphrase
// timeout, signing key missing) had its diff nuked even though the
// LLM had been paid for the work.
//
// "Empty" semantics:
//   - `git status --porcelain` returns no lines (no staged, no modified,
//     no untracked).
//   - `git rev-list HEAD ^@{u}` returns no commits ahead of upstream.
//     If there's no upstream tracked (fresh branch never pushed), we
//     fall back to comparing HEAD against the merge-base with `main`.
//
// On any error reading git state, we return FALSE (preserve the
// worktree). Better to leak a directory than wipe data we can't inspect.
func WorktreeIsEmpty(worktreeDir string) bool {
	if worktreeDir == "" {
		return true
	}
	// Uncommitted/untracked check.
	status, err := run(worktreeDir, "git", "status", "--porcelain")
	if err != nil {
		return false
	}
	if strings.TrimSpace(status) != "" {
		return false
	}
	// Commits-ahead check. Try upstream first; fall back to main.
	for _, base := range []string{"@{u}", "main", "master"} {
		out, err := run(worktreeDir, "git", "rev-list", "--count", "HEAD", "^"+base)
		if err != nil {
			continue
		}
		count := strings.TrimSpace(out)
		if count == "0" {
			return true
		}
		// Found a base, got a non-zero count → has commits → not empty.
		return false
	}
	// Couldn't resolve any base — be conservative and preserve.
	return false
}

// MergeConflictFiles returns the subset of paths that would actually
// conflict on a 3-way merge of `headRef` into `baseRef`, computed via
// `git merge-tree --name-only --merge-base $(merge-base) head base`.
// Returns the conflicting paths only — NOT every path touched by the
// branch — so the UI can show a focused list.
//
// Why this exists: the previous conflict UI listed every file the PR
// modified (via the GitHub PR-files API), implying every file was
// conflicting when usually only a couple actually are. Real merge-
// conflict detection requires asking git itself; the GitHub API
// doesn't expose per-file conflict status.
//
// Implementation uses `git merge-tree` in plumbing mode (no worktree
// mutation, no working-directory state). The `--name-only --merge-base`
// flags require git 2.38 (Oct 2022) or newer. On older git the call
// returns an error; the caller should fall back to a less-precise
// signal (e.g. the full PR file list) and surface a hint.
//
// repoPath is the main repo directory; baseRef and headRef are
// resolvable refs (e.g. "origin/main", "origin/feat/issue-N-...").
// The caller is responsible for `git fetch`ing first so both refs
// exist locally; this function does not mutate refs.
func MergeConflictFiles(repoPath, baseRef, headRef string) ([]string, error) {
	if repoPath == "" || baseRef == "" || headRef == "" {
		return nil, fmt.Errorf("MergeConflictFiles: repoPath/baseRef/headRef all required")
	}
	// `git merge-tree --name-only --merge-base=<mb> <base> <head>` output:
	//   - Line 1: tree SHA of the merge result (NOT a filename — must skip).
	//   - Subsequent lines: paths of files with conflicts, one per line.
	//   - Exit status 0 when there are NO conflicts (clean merge).
	//   - Exit status 1 when there ARE conflicts. THIS IS THE HAPPY PATH
	//     for our use case; it tells us the merge would conflict and
	//     gives us the list. Exit > 1 (128 etc.) is a real error
	//     (fatal git problem, refs missing, etc.).
	//
	// Previously we treated exit 1 as failure and fell back to the
	// GitHub PR-files API for every dirty PR — listing every file the
	// PR touched instead of the actually-conflicting subset. That's
	// the user-reported bug on PR #181 (showed 11 files when only
	// internal/types/types.go actually conflicts).
	mergeBase, err := run(repoPath, "git", "merge-base", baseRef, headRef)
	if err != nil {
		return nil, fmt.Errorf("merge-base %s %s: %w", baseRef, headRef, err)
	}
	mergeBase = strings.TrimSpace(mergeBase)
	if mergeBase == "" {
		return nil, fmt.Errorf("merge-base returned empty for %s..%s", baseRef, headRef)
	}
	out, exitCode, err := runWithExit(repoPath, "git",
		"merge-tree",
		"--name-only",
		"--merge-base="+mergeBase,
		baseRef, headRef)
	switch exitCode {
	case 0:
		// Clean merge — no conflicts. Caller already gates on
		// pr.MergeableState=='dirty' so this branch is rare in practice
		// but harmless: return empty list.
		return nil, nil
	case 1:
		// Conflict path — parse output. Continue below.
	default:
		// Real error: older git that doesn't support --merge-base,
		// missing refs, fatal repo state, etc. Surface to caller for
		// fallback to FetchPRFiles.
		if err != nil {
			return nil, fmt.Errorf("merge-tree %s %s: %w (exit %d)", baseRef, headRef, err, exitCode)
		}
		return nil, fmt.Errorf("merge-tree %s %s: unexpected exit %d", baseRef, headRef, exitCode)
	}

	// Parse the conflict-path list. The actual stdout format from
	// `git merge-tree --name-only` is:
	//
	//   <tree-sha>
	//   <path1>
	//   <path2>
	//   ...
	//                          <-- BLANK LINE (separator)
	//   Auto-merging <path1>   <-- human-readable summary; ignore
	//   CONFLICT (content): Merge conflict in <path1>
	//   ...
	//
	// We read line-by-line: skip the first non-empty line (tree SHA),
	// collect non-empty lines into `files`, and STOP at the first
	// blank line — anything after that is human-readable summary
	// (Auto-merging / CONFLICT) which would otherwise pollute the
	// list with non-paths.
	var files []string
	seenSHA := false
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimRight(line, "\r")
		if line == "" {
			if seenSHA {
				// Hit the separator after we've started reading paths.
				// Everything after this is summary noise — stop.
				break
			}
			// Leading blank lines (rare but tolerate) — skip.
			continue
		}
		if !seenSHA {
			seenSHA = true // tree SHA, discard
			continue
		}
		files = append(files, line)
	}
	return files, nil
}

// HasSubmodules reports whether the worktree has a .gitmodules file at its
// root. Called after Add (q4=D) so InitSubmodules only fires on repos that
// actually use submodules.
func HasSubmodules(worktreeDir string) bool {
	_, err := run(worktreeDir, "test", "-f", ".gitmodules")
	return err == nil
}

// InitSubmodules runs `git submodule update --init --recursive` from inside
// the worktree. Slow for large submodules; this is the documented cost of the
// q4=D "complete checkout" choice.
func InitSubmodules(worktreeDir string) error {
	out, err := run(worktreeDir, "git", "submodule", "update", "--init", "--recursive")
	if err != nil {
		return fmt.Errorf("submodule init: %w (output: %s)", err, out)
	}
	return nil
}

// RunInDir is the public version of the package's run helper. Lets callers
// outside this package execute git commands in a specific directory and
// receive (combinedStdout, error) without spawning their own exec wiring.
// Used by the GitHub poller to fetch refs into the local repo before
// asking MergeConflictFiles to compute conflicts.
func RunInDir(dir, name string, args ...string) (string, error) {
	return run(dir, name, args...)
}

func parseWorktreeList(out string) []Entry {
	var entries []Entry
	var cur Entry
	flush := func() {
		if cur.Path != "" {
			entries = append(entries, cur)
		}
		cur = Entry{}
	}
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			flush()
			continue
		}
		switch {
		case strings.HasPrefix(line, "worktree "):
			cur.Path = strings.TrimPrefix(line, "worktree ")
		case strings.HasPrefix(line, "HEAD "):
			cur.HEAD = strings.TrimPrefix(line, "HEAD ")
		case strings.HasPrefix(line, "branch "):
			cur.Branch = strings.TrimPrefix(line, "branch ")
		}
	}
	flush()
	return entries
}

func run(dir, name string, args ...string) (string, error) {
	cmd := exec.Command(name, args...)
	if dir != "" {
		cmd.Dir = dir
	}
	out, err := cmd.CombinedOutput()
	return string(out), err
}

// runWithExit is run() + the process exit code. Used by callers that need
// to distinguish meaningful non-zero exits (e.g. `git merge-tree` exits 1
// when there are conflicts — which is the case we actually want output
// for) from real errors. Returns STDOUT only (not combined output) so
// stderr noise like git's "Auto-merging X" / "CONFLICT" status lines
// doesn't pollute the parseable stdout. exitCode is 0 on success, the
// process exit on non-zero exit, or -1 if the process never started.
func runWithExit(dir, name string, args ...string) (string, int, error) {
	cmd := exec.Command(name, args...)
	if dir != "" {
		cmd.Dir = dir
	}
	out, err := cmd.Output()
	if err == nil {
		return string(out), 0, nil
	}
	if ee, ok := err.(*exec.ExitError); ok {
		return string(out), ee.ExitCode(), err
	}
	return string(out), -1, err
}
