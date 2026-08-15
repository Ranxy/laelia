-- Agent stop/start: an `enabled` flag distinguishes a stopped agent (its
-- machine runner is torn down and it processes no session messages) from a
-- deleted one. Soft-deleted rows remain and their handles are never reused.
-- Default TRUE so existing agents are all running.
-- Version: 1.1.21
ALTER TABLE agent ADD COLUMN IF NOT EXISTS enabled boolean NOT NULL DEFAULT TRUE;
