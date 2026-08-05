-- follow_owner_permissions: whether the agent inherits its owner's channel
-- read access (channels/DMs the owner can read). Default TRUE: the agent acts
-- within its owner's channel visibility; disable per-agent to restrict.
ALTER TABLE agent ADD COLUMN IF NOT EXISTS follow_owner_permissions boolean NOT NULL DEFAULT TRUE;
