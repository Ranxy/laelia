-- allow_add_to_channel: whether other users may add this agent to a channel.
-- Default FALSE = only the agent's creator or a workspace admin may add it.
ALTER TABLE agent ADD COLUMN IF NOT EXISTS allow_add_to_channel boolean NOT NULL DEFAULT FALSE;
