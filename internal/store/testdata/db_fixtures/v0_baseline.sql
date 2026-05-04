-- v0_baseline.sql: representative pre-framework schema for migration tests.
-- Matches the schema produced by the legacy migrate() as of the initial
-- migration framework introduction.  Used by migrations_test.go to create
-- a minimal "old binary" database and assert that the new migration framework
-- applies cleanly on top of it.

CREATE TABLE IF NOT EXISTS workspaces (id TEXT PRIMARY KEY, json TEXT NOT NULL);
CREATE TABLE IF NOT EXISTS goals (
    id TEXT PRIMARY KEY,
    workspace_id TEXT,
    status TEXT NOT NULL,
    json TEXT NOT NULL,
    "order" INTEGER NOT NULL DEFAULT 0
);
CREATE TABLE IF NOT EXISTS issues (
    workspace_id TEXT NOT NULL,
    number INTEGER NOT NULL,
    column_name TEXT NOT NULL,
    manual_order INTEGER,
    json TEXT NOT NULL,
    archived_at INTEGER,
    closed_at INTEGER,
    PRIMARY KEY (workspace_id, number)
);
CREATE TABLE IF NOT EXISTS plans (
    workspace_id TEXT NOT NULL,
    issue_number INTEGER NOT NULL,
    revision INTEGER NOT NULL,
    json TEXT NOT NULL,
    PRIMARY KEY (workspace_id, issue_number, revision)
);
CREATE TABLE IF NOT EXISTS sessions (
    id TEXT PRIMARY KEY,
    workspace_id TEXT NOT NULL,
    issue_number INTEGER NOT NULL,
    pid INTEGER,
    state TEXT NOT NULL,
    transcript_path TEXT,
    pending_question_id TEXT,
    json TEXT NOT NULL,
    transcript_offset INTEGER NOT NULL DEFAULT 0,
    acknowledged_at INTEGER,
    input_tokens INTEGER NOT NULL DEFAULT 0,
    output_tokens INTEGER NOT NULL DEFAULT 0,
    estimated_cost_cents REAL NOT NULL DEFAULT 0
);
CREATE TABLE IF NOT EXISTS events (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    type TEXT NOT NULL,
    ts INTEGER NOT NULL,
    payload TEXT
);
CREATE TABLE IF NOT EXISTS settings (key TEXT PRIMARY KEY, value TEXT NOT NULL);
CREATE TABLE IF NOT EXISTS labels (
    workspace_id TEXT NOT NULL,
    name         TEXT NOT NULL,
    color        TEXT NOT NULL,
    description  TEXT,
    PRIMARY KEY (workspace_id, name)
);
CREATE TABLE IF NOT EXISTS pools (
    id           TEXT PRIMARY KEY,
    name         TEXT NOT NULL,
    provider     TEXT NOT NULL,
    endpoint     TEXT NOT NULL DEFAULT '',
    model        TEXT NOT NULL,
    capacity     INTEGER NOT NULL DEFAULT 1,
    enabled      INTEGER NOT NULL DEFAULT 1,
    api_key      TEXT NOT NULL DEFAULT '',
    role         TEXT NOT NULL DEFAULT 'work' CHECK(role IN ('plan','work','orchestrator')),
    created_at   INTEGER NOT NULL,
    priority     INTEGER NOT NULL DEFAULT 0,
    temperature  REAL,
    max_turns    INTEGER,
    max_input_tokens INTEGER,
    bash_timeout_ms  INTEGER,
    output_cap       INTEGER,
    scope        TEXT NOT NULL DEFAULT 'shared',
    workspace_id TEXT NOT NULL DEFAULT ''
);
CREATE TABLE IF NOT EXISTS pending_pool_for (
    id           INTEGER PRIMARY KEY AUTOINCREMENT,
    workspace_id TEXT NOT NULL,
    issue_number INTEGER NOT NULL,
    role         TEXT NOT NULL,
    action       TEXT NOT NULL,
    created_at   INTEGER NOT NULL,
    UNIQUE(workspace_id, issue_number, role, action)
);
CREATE TABLE IF NOT EXISTS pool_usage (
    pool_id     TEXT NOT NULL,
    window      TEXT NOT NULL,
    limit_value INTEGER NOT NULL,
    used        INTEGER NOT NULL,
    resets_at   INTEGER NOT NULL,
    captured_at INTEGER NOT NULL,
    PRIMARY KEY (pool_id, window)
);
CREATE TABLE IF NOT EXISTS pr_comments (
    id           INTEGER PRIMARY KEY AUTOINCREMENT,
    workspace_id TEXT NOT NULL,
    issue_number INTEGER NOT NULL,
    comment_id   INTEGER NOT NULL,
    author       TEXT NOT NULL,
    body         TEXT NOT NULL,
    kind         TEXT NOT NULL,
    file_path    TEXT,
    line_number  INTEGER,
    created_at   INTEGER NOT NULL,
    read_at      INTEGER,
    pending_post INTEGER NOT NULL DEFAULT 0,
    UNIQUE(workspace_id, issue_number, comment_id)
);
CREATE INDEX IF NOT EXISTS idx_pr_comments_ws_issue ON pr_comments(workspace_id, issue_number);
