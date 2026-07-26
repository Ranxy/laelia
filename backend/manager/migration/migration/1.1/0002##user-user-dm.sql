-- User-to-user DM (conversation type 4 = USER_DM).
-- A type-4 conversation is a private 1:1 DM between exactly two users (no
-- agents). The initiator is the owner of record; both users are
-- conversation_member rows. user_dm_a/user_dm_b carry the ordered (a < b)
-- pair of principal.id values for race-free dedup via a partial unique index,
-- mirroring idx_conversation_agent_dm_unique for type-3 agent DMs. NULL for
-- type 1/2/3. type: 1=DM(user+agent), 2=channel, 3=AGENT_DM, 4=USER_DM.
ALTER TABLE conversation ADD COLUMN IF NOT EXISTS user_dm_a INTEGER REFERENCES principal(id) ON DELETE SET NULL;
ALTER TABLE conversation ADD COLUMN IF NOT EXISTS user_dm_b INTEGER REFERENCES principal(id) ON DELETE SET NULL;

DO $$
BEGIN
    IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conname = 'conversation_user_dm_order_check') THEN
        ALTER TABLE conversation ADD CONSTRAINT conversation_user_dm_order_check
            CHECK (user_dm_a IS NULL OR user_dm_b IS NULL OR user_dm_a < user_dm_b);
    END IF;
END $$;

CREATE UNIQUE INDEX IF NOT EXISTS idx_conversation_user_dm_unique
    ON conversation(user_dm_a, user_dm_b) WHERE type = 4;