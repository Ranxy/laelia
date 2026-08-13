-- Message emoji reactions (lightweight feedback).
--
-- A reaction is a sideband attribute of a message: it never bumps the
-- conversation's room version, never wakes agents, never counts as unread, and
-- never generates conversation activity. It lives entirely in this table, so
-- it is fully decoupled from chat_message's room-version / cursor / activity
-- machinery.
--
-- The actor is either a user (principal_id) or an agent (agent_id) — exactly
-- one is set (CHECK num_nonnulls = 1). Both actor columns must stay nullable,
-- so the "one row per (message, actor, emoji)" rule cannot be a composite
-- PRIMARY KEY: PK columns are implicitly NOT NULL in Postgres and would reject
-- the NULL actor column (agent rows have principal_id NULL and vice versa).
-- Instead two partial UNIQUE indexes enforce the rule — one for user actors,
-- one for agent actors. UNIQUE indexes treat NULLs as distinct, so the two
-- indexes never collide with each other, and each partial WHERE clause keeps
-- the index tight. A repeated add by the same actor on the same
-- (message, emoji) hits its index, so INSERT ... ON CONFLICT DO NOTHING is a
-- no-op; removes are naturally idempotent (a DELETE of a non-existent row is
-- a no-op).
CREATE TABLE IF NOT EXISTS message_reaction (
  message_id   uuid NOT NULL REFERENCES chat_message(id) ON DELETE CASCADE,
  principal_id int NULL REFERENCES principal(id),
  agent_id     int NULL REFERENCES agent(id),
  emoji        text NOT NULL,
  created_at   timestamptz NOT NULL DEFAULT now(),
  CONSTRAINT message_reaction_actor CHECK (num_nonnulls(principal_id, agent_id) = 1)
);
CREATE UNIQUE INDEX IF NOT EXISTS uq_message_reaction_user
  ON message_reaction (message_id, emoji, principal_id) WHERE principal_id IS NOT NULL;
CREATE UNIQUE INDEX IF NOT EXISTS uq_message_reaction_agent
  ON message_reaction (message_id, emoji, agent_id) WHERE agent_id IS NOT NULL;
CREATE INDEX IF NOT EXISTS idx_message_reaction_message ON message_reaction(message_id);
