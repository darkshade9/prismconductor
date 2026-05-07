//go:build ignore

// seed-db generates tests/fixtures/seed.db and tests/fixtures/workspaces.json
// — the canonical fixtures used by the visual smoke spec
// (tests/e2e/visual.spec.ts). Run from the repo root:
//
//	go run ./tests/scripts/seed-db.go [-out tests/fixtures/seed.db]
//
// The fixture contains two workspaces:
//   - "Seeded Project" — 5 cards spread across TODO/PLAN/IN_PROGRESS/REVIEW/DONE
//     plus a ready-to-execute plan on the PLAN card.
//   - "Empty Project" — no cards, used for the empty-board screenshot.
//
// workspaces.json is written alongside seed.db and must be copied to the
// PRISMCONDUCTOR_DATA_DIR by the CI workflow so ListWorkspaces() can serve
// the workspace chips in visual.spec.ts.
package main

import (
	"database/sql"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"os"
	"path/filepath"

	_ "modernc.org/sqlite"
)

func main() {
	out := flag.String("out", "tests/fixtures/seed.db", "output path for the fixture DB")
	flag.Parse()

	if err := os.MkdirAll(filepath.Dir(*out), 0o755); err != nil {
		log.Fatalf("mkdir: %v", err)
	}
	os.Remove(*out) // always rebuild from scratch

	db, err := sql.Open("sqlite", *out)
	if err != nil {
		log.Fatalf("open: %v", err)
	}
	defer db.Close()

	for _, p := range []string{
		"PRAGMA journal_mode = WAL",
		"PRAGMA foreign_keys = ON",
	} {
		if _, err := db.Exec(p); err != nil {
			log.Fatalf("pragma %s: %v", p, err)
		}
	}

	if err := applySchema(db); err != nil {
		log.Fatalf("schema: %v", err)
	}
	if err := seedData(db); err != nil {
		log.Fatalf("seed: %v", err)
	}

	// Write workspaces.json so the workspace registry (which reads from
	// workspaces.json, not the SQLite workspaces table) has the same data.
	wsOut := filepath.Join(filepath.Dir(*out), "workspaces.json")
	if err := writeWorkspacesJSON(wsOut); err != nil {
		log.Fatalf("workspaces.json: %v", err)
	}

	fmt.Printf("fixture written to %s and %s\n", *out, wsOut)
}

// workspaceFixture mirrors the minimal fields that workspace.Registry reads.
type workspaceFixture struct {
	ID            string                 `json:"id"`
	DisplayName   string                 `json:"display_name"`
	RepoPath      string                 `json:"repo_path"`
	GitHubOwner   string                 `json:"github_owner"`
	GitHubRepo    string                 `json:"github_repo"`
	DefaultBranch string                 `json:"default_branch"`
	Color         string                 `json:"color"`
	AgentEnv      map[string]interface{} `json:"agent_env"`
	SkillProfile  map[string]interface{} `json:"skill_profile"`
	Conventions   map[string]interface{} `json:"conventions"`
	Enabled       bool                   `json:"enabled"`
}

func writeWorkspacesJSON(path string) error {
	env := map[string]interface{}{"env_vars": map[string]interface{}{}, "pre_commands": []string{}, "shell": "/bin/bash"}
	sp := map[string]interface{}{
		"mode": "bundled", "use_conductor_plan": true, "use_conductor_execute": true,
		"use_conductor_close": false, "native_plan_command": "", "native_execute_command": "",
		"native_close_command": "", "extra_context_files": nil,
	}
	conv := map[string]interface{}{"test_command": "", "build_command": "", "lint_command": "", "py_env_path": "", "package_manager": ""}

	workspaces := []workspaceFixture{
		{
			ID: "seeded-ws", DisplayName: "Seeded Project",
			RepoPath: "/tmp/seeded-repo", GitHubOwner: "test", GitHubRepo: "seeded-repo",
			DefaultBranch: "main", Color: "#3b82f6",
			AgentEnv: env, SkillProfile: sp, Conventions: conv, Enabled: true,
		},
		{
			ID: "empty-ws", DisplayName: "Empty Project",
			RepoPath: "/tmp/empty-repo", GitHubOwner: "test", GitHubRepo: "empty-repo",
			DefaultBranch: "main", Color: "#64748b",
			AgentEnv: env, SkillProfile: sp, Conventions: conv, Enabled: true,
		},
	}

	b, err := json.MarshalIndent(workspaces, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, b, 0o644)
}

func applySchema(db *sql.DB) error {
	_, err := db.Exec(`
CREATE TABLE IF NOT EXISTS workspaces (
    id   TEXT PRIMARY KEY,
    json TEXT NOT NULL
);
CREATE TABLE IF NOT EXISTS goals (
    id           TEXT    PRIMARY KEY,
    workspace_id TEXT,
    status       TEXT    NOT NULL,
    json         TEXT    NOT NULL,
    "order"      INTEGER NOT NULL DEFAULT 0
);
CREATE TABLE IF NOT EXISTS issues (
    workspace_id TEXT    NOT NULL,
    number       INTEGER NOT NULL,
    column_name  TEXT    NOT NULL,
    manual_order INTEGER,
    json         TEXT    NOT NULL,
    archived_at  INTEGER,
    closed_at    INTEGER,
    PRIMARY KEY (workspace_id, number)
);
CREATE TABLE IF NOT EXISTS plans (
    workspace_id TEXT    NOT NULL,
    issue_number INTEGER NOT NULL,
    revision     INTEGER NOT NULL,
    json         TEXT    NOT NULL,
    PRIMARY KEY (workspace_id, issue_number, revision)
);
CREATE TABLE IF NOT EXISTS sessions (
    id                    TEXT    PRIMARY KEY,
    workspace_id          TEXT    NOT NULL,
    issue_number          INTEGER NOT NULL,
    pid                   INTEGER,
    state                 TEXT    NOT NULL,
    transcript_path       TEXT,
    pending_question_id   TEXT,
    json                  TEXT    NOT NULL,
    transcript_offset     INTEGER NOT NULL DEFAULT 0,
    acknowledged_at       INTEGER,
    input_tokens          INTEGER NOT NULL DEFAULT 0,
    output_tokens         INTEGER NOT NULL DEFAULT 0,
    estimated_cost_cents  REAL    NOT NULL DEFAULT 0
);
CREATE TABLE IF NOT EXISTS events (
    id      INTEGER PRIMARY KEY AUTOINCREMENT,
    type    TEXT    NOT NULL,
    ts      INTEGER NOT NULL,
    payload TEXT
);
CREATE TABLE IF NOT EXISTS settings (
    key   TEXT PRIMARY KEY,
    value TEXT NOT NULL
);
CREATE TABLE IF NOT EXISTS labels (
    workspace_id TEXT NOT NULL,
    name         TEXT NOT NULL,
    color        TEXT NOT NULL,
    description  TEXT,
    PRIMARY KEY (workspace_id, name)
);
CREATE TABLE IF NOT EXISTS pools (
    id           TEXT    PRIMARY KEY,
    name         TEXT    NOT NULL,
    provider     TEXT    NOT NULL,
    endpoint     TEXT    NOT NULL DEFAULT '',
    model        TEXT    NOT NULL,
    capacity     INTEGER NOT NULL DEFAULT 1,
    enabled      INTEGER NOT NULL DEFAULT 1,
    api_key      TEXT    NOT NULL DEFAULT '',
    role         TEXT    NOT NULL DEFAULT 'work' CHECK(role IN ('plan','work','orchestrator')),
    created_at   INTEGER NOT NULL,
    priority     INTEGER NOT NULL DEFAULT 0,
    temperature  REAL,
    max_turns    INTEGER,
    max_input_tokens  INTEGER,
    bash_timeout_ms   INTEGER,
    output_cap        INTEGER,
    scope             TEXT    NOT NULL DEFAULT 'shared',
    workspace_id      TEXT    NOT NULL DEFAULT ''
);
CREATE TABLE IF NOT EXISTS pending_pool_for (
    id           INTEGER PRIMARY KEY AUTOINCREMENT,
    workspace_id TEXT    NOT NULL,
    issue_number INTEGER NOT NULL,
    role         TEXT    NOT NULL,
    action       TEXT    NOT NULL,
    created_at   INTEGER NOT NULL,
    UNIQUE(workspace_id, issue_number, role, action)
);
CREATE TABLE IF NOT EXISTS pool_usage (
    pool_id     TEXT    NOT NULL,
    window      TEXT    NOT NULL,
    limit_value INTEGER NOT NULL,
    used        INTEGER NOT NULL,
    resets_at   INTEGER NOT NULL,
    captured_at INTEGER NOT NULL,
    PRIMARY KEY (pool_id, window)
);
CREATE TABLE IF NOT EXISTS pr_comments (
    id           INTEGER PRIMARY KEY AUTOINCREMENT,
    workspace_id TEXT    NOT NULL,
    issue_number INTEGER NOT NULL,
    comment_id   INTEGER NOT NULL,
    author       TEXT    NOT NULL,
    body         TEXT    NOT NULL,
    kind         TEXT    NOT NULL,
    file_path    TEXT,
    line_number  INTEGER,
    created_at   INTEGER NOT NULL,
    read_at      INTEGER,
    pending_post INTEGER NOT NULL DEFAULT 0,
    UNIQUE(workspace_id, issue_number, comment_id)
);
CREATE INDEX IF NOT EXISTS idx_pr_comments_ws_issue ON pr_comments(workspace_id, issue_number);
CREATE TABLE IF NOT EXISTS card_state_transitions (
    id              INTEGER PRIMARY KEY AUTOINCREMENT,
    workspace_id    TEXT    NOT NULL,
    issue_number    INTEGER NOT NULL,
    from_state      TEXT    NOT NULL,
    to_state        TEXT    NOT NULL,
    event_kind      TEXT    NOT NULL,
    details_json    TEXT,
    transitioned_at INTEGER NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_cst_workspace_issue ON card_state_transitions(workspace_id, issue_number);
CREATE TABLE IF NOT EXISTS collections (
    id         TEXT    PRIMARY KEY,
    name       TEXT    NOT NULL,
    context_md TEXT    NOT NULL DEFAULT '',
    created_at INTEGER NOT NULL,
    updated_at INTEGER NOT NULL
);
CREATE TABLE IF NOT EXISTS collection_members (
    collection_id TEXT    NOT NULL,
    workspace_id  TEXT    NOT NULL,
    position      INTEGER NOT NULL DEFAULT 0,
    PRIMARY KEY (collection_id, workspace_id),
    FOREIGN KEY (collection_id) REFERENCES collections(id) ON DELETE CASCADE
);
CREATE UNIQUE INDEX IF NOT EXISTS idx_collection_members_workspace ON collection_members(workspace_id);
CREATE TABLE IF NOT EXISTS schema_migrations (
    id TEXT PRIMARY KEY
);
`)
	return err
}

func mustJSON(v any) string {
	b, err := json.Marshal(v)
	if err != nil {
		panic(err)
	}
	return string(b)
}

func seedData(db *sql.DB) error {
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback() //nolint:errcheck

	// Mark all known migrations as applied so the app doesn't re-run them.
	for _, id := range []string{
		"20250504_00_initial_migration_framework",
		"20260505_00_add_card_state_transitions",
		"20260506_00_add_collections",
	} {
		if _, err := tx.Exec(`INSERT INTO schema_migrations(id) VALUES(?)`, id); err != nil {
			return fmt.Errorf("migration stamp %s: %w", id, err)
		}
	}
	// Prevent the pools-role-check rebuild (table already has CHECK constraint).
	if _, err := tx.Exec(`INSERT INTO settings(key,value) VALUES('pools_role_check_v1','1')`); err != nil {
		return err
	}

	// Workspace 1: seeded project with cards.
	seededWS := map[string]any{
		"id":             "seeded-ws",
		"display_name":   "Seeded Project",
		"repo_path":      "/tmp/seeded-repo",
		"github_owner":   "test",
		"github_repo":    "seeded-repo",
		"default_branch": "main",
		"color":          "#3b82f6",
		"agent_env":      map[string]any{"env_vars": map[string]any{}, "pre_commands": []string{}, "shell": "/bin/bash"},
		"skill_profile":  map[string]any{"mode": "bundled", "use_conductor_plan": true, "use_conductor_execute": true},
		"conventions":    map[string]any{"test_command": "", "build_command": ""},
		"enabled":        true,
	}
	if _, err := tx.Exec(`INSERT INTO workspaces(id,json) VALUES(?,?)`, "seeded-ws", mustJSON(seededWS)); err != nil {
		return err
	}

	// Workspace 2: empty project — no issues.
	emptyWS := map[string]any{
		"id":             "empty-ws",
		"display_name":   "Empty Project",
		"repo_path":      "/tmp/empty-repo",
		"github_owner":   "test",
		"github_repo":    "empty-repo",
		"default_branch": "main",
		"color":          "#64748b",
		"agent_env":      map[string]any{"env_vars": map[string]any{}, "pre_commands": []string{}, "shell": "/bin/bash"},
		"skill_profile":  map[string]any{"mode": "bundled", "use_conductor_plan": true, "use_conductor_execute": true},
		"conventions":    map[string]any{"test_command": "", "build_command": ""},
		"enabled":        true,
	}
	if _, err := tx.Exec(`INSERT INTO workspaces(id,json) VALUES(?,?)`, "empty-ws", mustJSON(emptyWS)); err != nil {
		return err
	}

	// One active goal for the seeded workspace.
	goal := map[string]any{
		"id":              "goal-q1",
		"workspace_id":    "seeded-ws",
		"title":           "Q1 Feature Completeness",
		"intent":          "Ship all priority-1 features before the Q1 release cut.",
		"acceptance_rule": "All P1 issues reach DONE.",
		"issue_filter":    map[string]any{"labels": []string{}, "milestone": "", "free_text": "", "includes": []int{}, "excludes": []int{}},
		"status":          "active",
		"order":           0,
		"created_at":      "2026-05-01T00:00:00Z",
		"achieved_at":     nil,
		"notes":           "",
	}
	if _, err := tx.Exec(
		`INSERT INTO goals(id,workspace_id,status,json,"order") VALUES(?,?,?,?,?)`,
		"goal-q1", "seeded-ws", "active", mustJSON(goal), 0,
	); err != nil {
		return err
	}

	type issueRow struct {
		number     int
		column     string
		closedAt   *int64
		archivedAt *int64
		json       map[string]any
	}

	issues := []issueRow{
		{
			number: 101, column: "todo",
			json: map[string]any{
				"number": 101, "workspace_id": "seeded-ws",
				"title": "Add user authentication", "body": "Implement login / signup flow.",
				"labels": []string{"enhancement", "backend"}, "state": "open",
				"url": "https://github.com/test/seeded-repo/issues/101",
				"updated_at": "2026-05-01T10:00:00Z",
				"goal_id": "goal-q1", "priority": 0.85,
				"dependencies": []int{}, "dep_rationale": "", "column": "todo",
				"plan": nil, "session_id": nil, "last_error": "",
				"pr_number": nil, "pr_url": "",
				"archived_at": nil, "closed_at": nil,
				"waiting_for_pool": false,
				"work_seconds": 0, "work_seconds_plan": 0, "work_seconds_execute": 0, "cost_usd": 0.0,
				"needs_pr_info": nil, "failure_reason": "",
			},
		},
		{
			number: 102, column: "plan",
			json: map[string]any{
				"number": 102, "workspace_id": "seeded-ws",
				"title": "Refactor database schema", "body": "Migrate from MongoDB to PostgreSQL.",
				"labels": []string{"refactor", "backend"}, "state": "open",
				"url": "https://github.com/test/seeded-repo/issues/102",
				"updated_at": "2026-05-01T11:00:00Z",
				"goal_id": "goal-q1", "priority": 0.65,
				"dependencies": []int{}, "dep_rationale": "", "column": "plan",
				"plan": nil, "session_id": "sess-plan-1", "last_error": "",
				"pr_number": nil, "pr_url": "",
				"archived_at": nil, "closed_at": nil,
				"waiting_for_pool": false,
				"work_seconds": 0, "work_seconds_plan": 0, "work_seconds_execute": 0, "cost_usd": 0.0,
				"needs_pr_info": nil, "failure_reason": "",
			},
		},
		{
			number: 103, column: "in_progress",
			json: map[string]any{
				"number": 103, "workspace_id": "seeded-ws",
				"title": "Implement caching layer", "body": "Add Redis caching for API responses.",
				"labels": []string{"enhancement", "backend", "performance"}, "state": "open",
				"url": "https://github.com/test/seeded-repo/issues/103",
				"updated_at": "2026-05-01T12:00:00Z",
				"goal_id": "goal-q1", "priority": 0.72,
				"dependencies": []int{}, "dep_rationale": "", "column": "in_progress",
				"plan": nil, "session_id": "sess-exec-1", "last_error": "",
				"pr_number": nil, "pr_url": "",
				"archived_at": nil, "closed_at": nil,
				"waiting_for_pool": false,
				"work_seconds": 1800, "work_seconds_plan": 600, "work_seconds_execute": 1200, "cost_usd": 0.05,
				"needs_pr_info": nil, "failure_reason": "",
			},
		},
		{
			number: 104, column: "review",
			json: map[string]any{
				"number": 104, "workspace_id": "seeded-ws",
				"title": "Add email notifications", "body": "Send email alerts for important events.",
				"labels": []string{"feature", "frontend"}, "state": "open",
				"url": "https://github.com/test/seeded-repo/issues/104",
				"updated_at": "2026-05-02T10:00:00Z",
				"goal_id": "goal-q1", "priority": 0.58,
				"dependencies": []int{}, "dep_rationale": "", "column": "review",
				"plan": nil, "session_id": "sess-exec-2", "last_error": "",
				"pr_number": 15, "pr_url": "https://github.com/test/seeded-repo/pull/15",
				"archived_at": nil, "closed_at": nil,
				"waiting_for_pool": false,
				"work_seconds": 3600, "work_seconds_plan": 900, "work_seconds_execute": 2700, "cost_usd": 0.12,
				"needs_pr_info": nil, "failure_reason": "",
			},
		},
		{
			number: 105, column: "done",
			closedAt: int64ptr(1746022800),
			json: map[string]any{
				"number": 105, "workspace_id": "seeded-ws",
				"title": "Write API documentation", "body": "Document all REST endpoints with examples.",
				"labels": []string{"documentation"}, "state": "closed",
				"url": "https://github.com/test/seeded-repo/issues/105",
				"updated_at": "2026-04-30T15:00:00Z",
				"goal_id": "goal-q1", "priority": 0.4,
				"dependencies": []int{}, "dep_rationale": "", "column": "done",
				"plan": nil, "session_id": "sess-exec-3", "last_error": "",
				"pr_number": 14, "pr_url": "https://github.com/test/seeded-repo/pull/14",
				"archived_at": nil, "closed_at": "2026-04-30T15:00:00Z",
				"waiting_for_pool": false,
				"work_seconds": 2400, "work_seconds_plan": 600, "work_seconds_execute": 1800, "cost_usd": 0.08,
				"needs_pr_info": nil, "failure_reason": "",
			},
		},
	}

	for _, iss := range issues {
		if _, err := tx.Exec(
			`INSERT INTO issues(workspace_id,number,column_name,json,archived_at,closed_at) VALUES(?,?,?,?,?,?)`,
			"seeded-ws", iss.number, iss.column, mustJSON(iss.json), iss.archivedAt, iss.closedAt,
		); err != nil {
			return fmt.Errorf("issue %d: %w", iss.number, err)
		}
	}

	// Ready-to-execute plan for issue #102.
	plan := map[string]any{
		"issue_number": 102, "workspace_id": "seeded-ws", "revision": 1,
		"goal_summary":      "Migrate production database from MongoDB to PostgreSQL for improved query performance and reduced operational costs.",
		"executive_summary": "Set up PostgreSQL, design normalized schema, write migration scripts, and add integration tests. Result: 40% lower infrastructure spend and 2-3× faster queries.",
		"plan_markdown": `## Steps

1. Set up PostgreSQL dev environment
2. Design normalized schema for existing data
3. Write migration scripts
4. Add integration tests
5. Document the procedure

## Files to modify

- ` + "`db/schema.sql`" + ` (new)
- ` + "`migrations/001_postgres_init.sql`" + ` (new)
- ` + "`lib/database.py`" + ` (modify)
- ` + "`tests/test_database.py`" + ` (add)`,
		"files_to_modify": []map[string]string{
			{"path": "db/schema.sql", "intent": "add"},
			{"path": "migrations/001_postgres_init.sql", "intent": "add"},
			{"path": "lib/database.py", "intent": "modify"},
			{"path": "tests/test_database.py", "intent": "add"},
		},
		"dependencies_detected": []int{},
		"suggested_labels":      []string{"backend", "database"},
		"questions":             []any{},
		"estimated_complexity":  "large",
		"ready_to_execute":      true,
		"generated_at":          "2026-05-01T13:00:00Z",
		"approved_at":           nil,
	}
	if _, err := tx.Exec(
		`INSERT INTO plans(workspace_id,issue_number,revision,json) VALUES(?,?,?,?)`,
		"seeded-ws", 102, 1, mustJSON(plan),
	); err != nil {
		return err
	}

	// Sessions.
	type sessRow struct {
		id          string
		issueNumber int
		state       string
		json        map[string]any
	}
	sessions := []sessRow{
		{
			id: "sess-plan-1", issueNumber: 102, state: "completed",
			json: map[string]any{
				"id": "sess-plan-1", "workspace_id": "seeded-ws", "issue_number": 102,
				"mode": "plan", "state": "completed",
				"started_at": "2026-05-01T12:00:00Z", "ended_at": "2026-05-01T12:15:00Z",
				"pid": 0, "last_prompt": "Analyze schema differences between MongoDB and PostgreSQL.",
				"blocked_reason": "", "pending_question_id": "", "pool_id": "",
				"acknowledged_at": nil,
				"input_tokens": 2500, "output_tokens": 1800, "estimated_cost_cents": 3.2,
				"exit_code": 0, "signal": "", "branch": "",
			},
		},
		{
			id: "sess-exec-1", issueNumber: 103, state: "running",
			json: map[string]any{
				"id": "sess-exec-1", "workspace_id": "seeded-ws", "issue_number": 103,
				"mode": "execute", "state": "running",
				"started_at": "2026-05-01T13:00:00Z", "ended_at": nil,
				"pid": 0, "last_prompt": "Running integration tests for Redis cache implementation.",
				"blocked_reason": "", "pending_question_id": "", "pool_id": "",
				"acknowledged_at": nil,
				"input_tokens": 4200, "output_tokens": 2100, "estimated_cost_cents": 5.8,
				"exit_code": nil, "signal": "", "branch": "feat/redis-cache",
			},
		},
		{
			id: "sess-exec-2", issueNumber: 104, state: "completed",
			json: map[string]any{
				"id": "sess-exec-2", "workspace_id": "seeded-ws", "issue_number": 104,
				"mode": "execute", "state": "completed",
				"started_at": "2026-05-02T08:00:00Z", "ended_at": "2026-05-02T09:30:00Z",
				"pid": 0, "last_prompt": "PR opened: feat/email-notifications",
				"blocked_reason": "", "pending_question_id": "", "pool_id": "",
				"acknowledged_at": nil,
				"input_tokens": 7800, "output_tokens": 3400, "estimated_cost_cents": 10.2,
				"exit_code": 0, "signal": "", "branch": "feat/email-notifications",
			},
		},
		{
			id: "sess-exec-3", issueNumber: 105, state: "completed",
			json: map[string]any{
				"id": "sess-exec-3", "workspace_id": "seeded-ws", "issue_number": 105,
				"mode": "execute", "state": "completed",
				"started_at": "2026-04-30T13:00:00Z", "ended_at": "2026-04-30T14:40:00Z",
				"pid": 0, "last_prompt": "Documentation PR merged.",
				"blocked_reason": "", "pending_question_id": "", "pool_id": "",
				"acknowledged_at": nil,
				"input_tokens": 6100, "output_tokens": 2900, "estimated_cost_cents": 8.5,
				"exit_code": 0, "signal": "", "branch": "feat/api-docs",
			},
		},
	}
	for _, s := range sessions {
		if _, err := tx.Exec(
			`INSERT INTO sessions(id,workspace_id,issue_number,pid,state,json,transcript_offset) VALUES(?,?,?,?,?,?,?)`,
			s.id, "seeded-ws", s.issueNumber, 0, s.state, mustJSON(s.json), 0,
		); err != nil {
			return fmt.Errorf("session %s: %w", s.id, err)
		}
	}

	return tx.Commit()
}

func int64ptr(v int64) *int64 { return &v }
