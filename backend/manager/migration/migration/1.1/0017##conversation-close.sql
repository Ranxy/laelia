-- Per-user conversation closing (hide from the left-rail list). Adds
-- closed/closed_at to conversation_member_meta so a user can hide a channel or
-- DM from their left-rail list without deleting the conversation or its
-- messages. The table's PK (conversation_id, member_type, member_id) makes the
-- flag per-(user,conversation): each user has their own close state. A new
-- main-channel message (thread_root_message_id IS NULL) clears the flag for
-- every member of the conversation, so a closed chat reappears automatically
-- when it gets new activity; thread replies do not. Applies to all
-- conversation types (channels and DMs) since every user membership is a
-- conversation_member_meta row.
ALTER TABLE conversation_member_meta ADD COLUMN IF NOT EXISTS closed BOOLEAN NOT NULL DEFAULT false;
ALTER TABLE conversation_member_meta ADD COLUMN IF NOT EXISTS closed_at TIMESTAMPTZ;
