-- Per-user conversation pinning. Adds pinned/pinned_at to conversation_member
-- so a user can pin a channel or DM to the top of their left-rail list. The
-- table's PK (conversation_id, member_type, member_id) makes the flag
-- per-(user,conversation): each user has their own pins. pinned_at drives
-- stable ordering within the pinned group (most-recently-pinned first),
-- independent of conversation.updated_at so pinned items don't drift as new
-- messages arrive. Applies to all conversation types (channels and DMs) since
-- every user membership is a conversation_member row.
ALTER TABLE conversation_member ADD COLUMN IF NOT EXISTS pinned BOOLEAN NOT NULL DEFAULT false;
ALTER TABLE conversation_member ADD COLUMN IF NOT EXISTS pinned_at TIMESTAMPTZ;