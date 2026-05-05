-- Issue #194: persist subprocess exit diagnostics on failed execute sessions.
ALTER TABLE sessions ADD COLUMN exit_code INTEGER DEFAULT NULL;
ALTER TABLE sessions ADD COLUMN signal TEXT DEFAULT NULL;
ALTER TABLE sessions ADD COLUMN cmd_wait_time_ms INTEGER DEFAULT 0;
ALTER TABLE sessions ADD COLUMN last_stderr_chunk TEXT DEFAULT NULL;
