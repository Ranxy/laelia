-- Agent public description: a short intro shown to other users and agents
-- (what the agent is responsible for, its role). It is distinct from the
-- private persona_prompt (the agent's self prompt) and is never injected into
-- the agent's own prompt.
-- Version: 1.1.23
ALTER TABLE agent ADD COLUMN IF NOT EXISTS description text NOT NULL DEFAULT '';
