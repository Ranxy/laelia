-- can_manage_channel_members: whether the agent may add/remove members in a
-- channel where its owner is a channel Admin/Owner. Default TRUE: the agent
-- acts on its owner's behalf for member management; disable per-agent to
-- restrict. Independent of follow_owner_permissions (which controls read
-- visibility).
ALTER TABLE agent ADD COLUMN IF NOT EXISTS can_manage_channel_members boolean NOT NULL DEFAULT TRUE;
