-- Conversation archiving (owner-level). Adds archived/archived_at to the
-- conversation table itself (NOT conversation_member_meta, unlike the per-user
-- close flag) because archiving is an owner-level operation that applies to
-- the whole conversation and every member. An archived channel: is hidden from
-- the members-page channels roster, is not returned to agents by
-- ListChannelsForAgent, and rejects new messages from all members, but its
-- messages remain searchable via SearchChatHistory (search does not filter on
-- this column). Idempotent so re-applying the schema is safe.
ALTER TABLE conversation ADD COLUMN IF NOT EXISTS archived BOOLEAN NOT NULL DEFAULT false;
ALTER TABLE conversation ADD COLUMN IF NOT EXISTS archived_at TIMESTAMPTZ;
CREATE INDEX IF NOT EXISTS idx_conversation_archived ON conversation(archived) WHERE archived = true;
