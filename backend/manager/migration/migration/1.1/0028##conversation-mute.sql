-- Per-user conversation muting. Adds muted/muted_at to conversation_member_meta
-- so a user can silence notifications/activity for a channel or DM while
-- keeping unread counts and the left-rail row. The table's PK
-- (conversation_id, member_type, member_id) makes the flag per-(user,
-- conversation): each user has their own mute state. Applies to all
-- conversation types (channels and DMs) since every user membership is a
-- conversation_member_meta row.
ALTER TABLE conversation_member_meta ADD COLUMN IF NOT EXISTS muted BOOLEAN NOT NULL DEFAULT false;
ALTER TABLE conversation_member_meta ADD COLUMN IF NOT EXISTS muted_at TIMESTAMPTZ;
