-- user_group moves to a stable id primary key; email becomes optional (groups
-- without an email are referenced by their id). Safe for existing rows: every
-- row gets a generated id before the PK switch.
ALTER TABLE user_group ADD COLUMN IF NOT EXISTS id text;

UPDATE user_group SET id = gen_random_uuid()::text WHERE id IS NULL;

ALTER TABLE user_group ALTER COLUMN id SET NOT NULL;
ALTER TABLE user_group DROP CONSTRAINT IF EXISTS user_group_pkey;
ALTER TABLE user_group ADD PRIMARY KEY (id);

ALTER TABLE user_group ALTER COLUMN email DROP NOT NULL;
CREATE UNIQUE INDEX IF NOT EXISTS idx_user_group_email_unique ON user_group(email) WHERE email IS NOT NULL;
