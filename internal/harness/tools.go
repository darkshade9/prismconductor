package harness

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"prismconductor/internal/llm"
)

// env holds per-session tool state. Cwd is the worker's working directory
// (the worktree for execute-mode, the repo root for plan-mode); every path
// argument is resolved relative to it and rejected if it escapes.
type env struct {
	Cwd     string
	EnvVars []string
	Budget  Budget

	// Per-session in-memory todo scratchpad (q-todowrite). Not surfaced to the
	// conductor — the model writes here for its own bookkeeping; the harness
	// emits a @sys transcript line on each update so the user can see the
	// list mutating.
	todos []TodoItem
}

// TodoItem mirrors Claude Code's TodoWrite tool entry. Kept minimal: the
// harness never persists or queries this, just stores it in memory.
type TodoItem struct {
	Content    string `json:"content"`
	Status     string `json:"status"`
	ActiveForm string `json:"activeForm"`
}

type toolFn func(ctx context.Context, e *env, raw json.RawMessage) (string, error)

var registry = map[string]toolFn{
	"Read":      toolRead,
	"Write":     toolWrite,
	"Edit":      toolEdit,
	"Bash":      toolBash,
	"Glob":      toolGlob,
	"Grep":      toolGrep,
	"TodoWrite": toolTodoWrite,
}

// Definitions returns the JSONSchema-shaped tool descriptors the harness
// passes to Provider.ToolChat as ChatRequest.Tools (issue #58).
func Definitions() []llm.ToolDef {
	return []llm.ToolDef{
		{
			Name:        "Read",
			Description: "Read a file from the workspace. Optionally slice by line offset/limit.",
			Parameters: schema(map[string]any{
				"file_path": map[string]any{"type": "string", "description": "Path relative to the worktree."},
				"offset":    map[string]any{"type": "integer", "description": "Skip N lines from the top.", "default": 0},
				"limit":     map[string]any{"type": "integer", "description": "Read at most N lines.", "default": 0},
			}, []string{"file_path"}),
		},
		{
			Name:        "Write",
			Description: "Write a file in the workspace, creating parent dirs as needed. Atomic.",
			Parameters: schema(map[string]any{
				"file_path": map[string]any{"type": "string"},
				"content":   map[string]any{"type": "string"},
			}, []string{"file_path", "content"}),
		},
		{
			Name:        "Edit",
			Description: "Replace the first occurrence of old_string with new_string in file_path. Use replace_all=true to replace every occurrence.",
			Parameters: schema(map[string]any{
				"file_path":   map[string]any{"type": "string"},
				"old_string":  map[string]any{"type": "string"},
				"new_string":  map[string]any{"type": "string"},
				"replace_all": map[string]any{"type": "boolean", "default": false},
			}, []string{"file_path", "old_string", "new_string"}),
		},
		{
			Name:        "Bash",
			Description: "Run a shell command via /bin/bash -c with the worktree as cwd. Output is captured (stdout + stderr merged), capped at the harness output limit.",
			Parameters: schema(map[string]any{
				"command": map[string]any{"type": "string"},
				"timeout": map[string]any{"type": "integer", "description": "Seconds before killing the command. Defaults to the harness BashTimeout."},
			}, []string{"command"}),
		},
		{
			Name:        "Glob",
			Description: "Match files in the workspace by glob pattern. Returns up to 200 paths.",
			Parameters: schema(map[string]any{
				"pattern": map[string]any{"type": "string"},
				"path":    map[string]any{"type": "string", "description": "Subdirectory to scope the match. Defaults to worktree root."},
			}, []string{"pattern"}),
		},
		{
			Name:        "Grep",
			Description: "Search file contents for a regex. Returns up to 200 match lines with file:line prefixes.",
			Parameters: schema(map[string]any{
				"pattern": map[string]any{"type": "string"},
				"path":    map[string]any{"type": "string"},
				"glob":    map[string]any{"type": "string", "description": "File-name glob filter."},
			}, []string{"pattern"}),
		},
		{
			Name:        "TodoWrite",
			Description: "Replace the harness's per-session todo list. Each item has content, status (pending|in_progress|completed), and activeForm.",
			Parameters: schema(map[string]any{
				"todos": map[string]any{
					"type": "array",
					"items": map[string]any{
						"type": "object",
						"properties": map[string]any{
							"content":    map[string]any{"type": "string"},
							"status":     map[string]any{"type": "string"},
							"activeForm": map[string]any{"type": "string"},
						},
						"required": []string{"content", "status", "activeForm"},
					},
				},
			}, []string{"todos"}),
		},
	}
}

func schema(props map[string]any, required []string) json.RawMessage {
	obj := map[string]any{
		"type":       "object",
		"properties": props,
	}
	if len(required) > 0 {
		obj["required"] = required
	}
	b, _ := json.Marshal(obj)
	return b
}

// dispatch runs the named tool. Unknown names return an error string the
// model is expected to recover from. Tool errors are returned as the second
// return; the caller serializes them as is_error tool_results.
func dispatch(ctx context.Context, e *env, name string, raw json.RawMessage) (string, error) {
	fn, ok := registry[name]
	if !ok {
		return "", fmt.Errorf("unknown tool %q", name)
	}
	return fn(ctx, e, raw)
}

// resolvePath turns user-supplied input into an absolute path inside the
// worker's cwd. Paths whose canonical form escapes cwd are rejected.
func resolvePath(e *env, p string) (string, error) {
	if p == "" {
		return "", errors.New("empty path")
	}
	if !filepath.IsAbs(p) {
		p = filepath.Join(e.Cwd, p)
	}
	abs, err := filepath.Abs(p)
	if err != nil {
		return "", err
	}
	rel, err := filepath.Rel(e.Cwd, abs)
	if err != nil {
		return "", err
	}
	if rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("path %s escapes worktree", p)
	}
	return abs, nil
}

const readCap = 200 * 1024

func toolRead(_ context.Context, e *env, raw json.RawMessage) (string, error) {
	var args struct {
		FilePath string `json:"file_path"`
		Offset   int    `json:"offset"`
		Limit    int    `json:"limit"`
	}
	if err := json.Unmarshal(raw, &args); err != nil {
		return "", err
	}
	abs, err := resolvePath(e, args.FilePath)
	if err != nil {
		return "", err
	}
	b, err := os.ReadFile(abs)
	if err != nil {
		return "", err
	}
	s := string(b)
	if args.Offset > 0 || args.Limit > 0 {
		lines := strings.Split(s, "\n")
		start := args.Offset
		if start > len(lines) {
			start = len(lines)
		}
		end := len(lines)
		if args.Limit > 0 && start+args.Limit < end {
			end = start + args.Limit
		}
		s = strings.Join(lines[start:end], "\n")
	}
	if len(s) > readCap {
		s = s[:readCap] + "\n… (truncated)"
	}
	return s, nil
}

func toolWrite(_ context.Context, e *env, raw json.RawMessage) (string, error) {
	var args struct {
		FilePath string `json:"file_path"`
		Content  string `json:"content"`
	}
	if err := json.Unmarshal(raw, &args); err != nil {
		return "", err
	}
	abs, err := resolvePath(e, args.FilePath)
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
		return "", err
	}
	tmp, err := os.CreateTemp(filepath.Dir(abs), ".harness-*")
	if err != nil {
		return "", err
	}
	tmpName := tmp.Name()
	if _, err := tmp.WriteString(args.Content); err != nil {
		tmp.Close()
		os.Remove(tmpName)
		return "", err
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmpName)
		return "", err
	}
	if err := os.Rename(tmpName, abs); err != nil {
		os.Remove(tmpName)
		return "", err
	}
	return fmt.Sprintf("wrote %d bytes to %s", len(args.Content), args.FilePath), nil
}

func toolEdit(_ context.Context, e *env, raw json.RawMessage) (string, error) {
	var args struct {
		FilePath   string `json:"file_path"`
		OldString  string `json:"old_string"`
		NewString  string `json:"new_string"`
		ReplaceAll bool   `json:"replace_all"`
	}
	if err := json.Unmarshal(raw, &args); err != nil {
		return "", err
	}
	abs, err := resolvePath(e, args.FilePath)
	if err != nil {
		return "", err
	}
	b, err := os.ReadFile(abs)
	if err != nil {
		return "", err
	}
	src := string(b)
	if args.OldString == "" {
		return "", errors.New("old_string is empty")
	}
	count := strings.Count(src, args.OldString)
	if count == 0 {
		return "", fmt.Errorf("old_string not found in %s", args.FilePath)
	}
	var out string
	if args.ReplaceAll {
		out = strings.ReplaceAll(src, args.OldString, args.NewString)
	} else {
		if count > 1 {
			return "", fmt.Errorf("old_string matches %d times in %s; pass replace_all=true or extend old_string for uniqueness", count, args.FilePath)
		}
		out = strings.Replace(src, args.OldString, args.NewString, 1)
	}
	tmp, err := os.CreateTemp(filepath.Dir(abs), ".harness-*")
	if err != nil {
		return "", err
	}
	tmpName := tmp.Name()
	if _, err := tmp.WriteString(out); err != nil {
		tmp.Close()
		os.Remove(tmpName)
		return "", err
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmpName)
		return "", err
	}
	if err := os.Rename(tmpName, abs); err != nil {
		os.Remove(tmpName)
		return "", err
	}
	if args.ReplaceAll {
		return fmt.Sprintf("replaced %d occurrences in %s", count, args.FilePath), nil
	}
	return fmt.Sprintf("edited %s", args.FilePath), nil
}

func toolBash(ctx context.Context, e *env, raw json.RawMessage) (string, error) {
	var args struct {
		Command string `json:"command"`
		Timeout int    `json:"timeout"`
	}
	if err := json.Unmarshal(raw, &args); err != nil {
		return "", err
	}
	if args.Command == "" {
		return "", errors.New("empty command")
	}
	timeout := e.Budget.BashTimeout
	if timeout <= 0 {
		timeout = 5 * time.Minute
	}
	if args.Timeout > 0 {
		timeout = time.Duration(args.Timeout) * time.Second
	}
	cctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	cmd := exec.CommandContext(cctx, "/bin/bash", "-c", args.Command)
	cmd.Dir = e.Cwd
	cmd.Env = append(os.Environ(), e.EnvVars...)
	var buf bytes.Buffer
	cmd.Stdout = &buf
	cmd.Stderr = &buf
	err := cmd.Run()
	out := buf.String()
	cap := e.Budget.OutputCap
	if cap <= 0 {
		cap = 50 * 1024
	}
	truncated := false
	if len(out) > cap {
		out = out[:cap]
		truncated = true
	}
	header := fmt.Sprintf("$ %s\nexit=%d", args.Command, cmd.ProcessState.ExitCode())
	if cctx.Err() == context.DeadlineExceeded {
		header += " (timed out)"
	}
	if truncated {
		header += " (output truncated)"
	}
	body := header + "\n" + out
	if err != nil && cctx.Err() != context.DeadlineExceeded {
		// Non-zero exit. Don't surface as a tool error — the model often
		// expects to read a failing exit code (e.g. running the test suite to
		// see what failed). Return the captured output instead.
		return body, nil
	}
	return body, nil
}

const globCap = 200
const grepCap = 200

func toolGlob(_ context.Context, e *env, raw json.RawMessage) (string, error) {
	var args struct {
		Pattern string `json:"pattern"`
		Path    string `json:"path"`
	}
	if err := json.Unmarshal(raw, &args); err != nil {
		return "", err
	}
	if args.Pattern == "" {
		return "", errors.New("empty pattern")
	}
	root := e.Cwd
	if args.Path != "" {
		abs, err := resolvePath(e, args.Path)
		if err != nil {
			return "", err
		}
		root = abs
	}
	type entry struct {
		path string
		mod  time.Time
	}
	var matches []entry
	err := filepath.WalkDir(root, func(p string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return nil
		}
		if d.IsDir() {
			if strings.HasPrefix(d.Name(), ".") && p != root {
				return filepath.SkipDir
			}
			return nil
		}
		rel, err := filepath.Rel(e.Cwd, p)
		if err != nil {
			return nil
		}
		ok, _ := filepath.Match(args.Pattern, filepath.Base(p))
		if !ok {
			ok, _ = filepath.Match(args.Pattern, rel)
		}
		if !ok {
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return nil
		}
		matches = append(matches, entry{path: rel, mod: info.ModTime()})
		return nil
	})
	if err != nil {
		return "", err
	}
	sort.Slice(matches, func(i, j int) bool {
		return matches[i].mod.After(matches[j].mod)
	})
	if len(matches) > globCap {
		matches = matches[:globCap]
	}
	out := make([]string, len(matches))
	for i, m := range matches {
		out[i] = m.path
	}
	if len(out) == 0 {
		return "(no matches)", nil
	}
	return strings.Join(out, "\n"), nil
}

func toolGrep(_ context.Context, e *env, raw json.RawMessage) (string, error) {
	var args struct {
		Pattern string `json:"pattern"`
		Path    string `json:"path"`
		Glob    string `json:"glob"`
	}
	if err := json.Unmarshal(raw, &args); err != nil {
		return "", err
	}
	if args.Pattern == "" {
		return "", errors.New("empty pattern")
	}
	re, err := regexp.Compile(args.Pattern)
	if err != nil {
		return "", err
	}
	root := e.Cwd
	if args.Path != "" {
		abs, err := resolvePath(e, args.Path)
		if err != nil {
			return "", err
		}
		root = abs
	}
	var matches []string
	err = filepath.WalkDir(root, func(p string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return nil
		}
		if d.IsDir() {
			if strings.HasPrefix(d.Name(), ".") && p != root {
				return filepath.SkipDir
			}
			return nil
		}
		if args.Glob != "" {
			ok, _ := filepath.Match(args.Glob, filepath.Base(p))
			if !ok {
				return nil
			}
		}
		b, err := os.ReadFile(p)
		if err != nil {
			return nil
		}
		rel, _ := filepath.Rel(e.Cwd, p)
		for i, line := range strings.Split(string(b), "\n") {
			if !re.MatchString(line) {
				continue
			}
			matches = append(matches, fmt.Sprintf("%s:%d:%s", rel, i+1, line))
			if len(matches) >= grepCap {
				return filepath.SkipAll
			}
		}
		return nil
	})
	if err != nil {
		return "", err
	}
	if len(matches) == 0 {
		return "(no matches)", nil
	}
	return strings.Join(matches, "\n"), nil
}

func toolTodoWrite(_ context.Context, e *env, raw json.RawMessage) (string, error) {
	var args struct {
		Todos []TodoItem `json:"todos"`
	}
	if err := json.Unmarshal(raw, &args); err != nil {
		return "", err
	}
	e.todos = args.Todos
	lines := make([]string, len(args.Todos))
	for i, t := range args.Todos {
		lines[i] = fmt.Sprintf("[%s] %s", t.Status, t.Content)
	}
	return fmt.Sprintf("todo list updated (%d items)\n%s", len(args.Todos), strings.Join(lines, "\n")), nil
}
