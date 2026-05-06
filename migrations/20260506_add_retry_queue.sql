-- Issue #221 Phase 1: persist retry metadata on execute sessions so retried
-- sessions are identifiable in the session log and cost rollups.
ALTER TABLE sessions ADD COLUMN retry_attempt INTEGER NOT NULL DEFAULT 0;
ALTER TABLE sessions ADD COLUMN retry_of_session_id TEXT DEFAULT NULL;
