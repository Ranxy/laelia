-- Agent teams: a named collection of agents that work together on tasks.
-- A team belongs to its creator (owner) and is visible to other users; only
-- the owner or a workspace admin may manage it. An agent can belong to at
-- most one team. A team can be assigned to at most one active (non-DONE) task.

CREATE TABLE IF NOT EXISTS agent_team (
    id            UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    resource_id   TEXT NOT NULL UNIQUE,
    name          TEXT NOT NULL,
    description   TEXT NOT NULL DEFAULT '',
    team_prompt   TEXT NOT NULL DEFAULT '',
    leader_agent_id INTEGER REFERENCES agent(id) ON DELETE SET NULL,
    owner_id      INTEGER NOT NULL REFERENCES principal(id),
    created_by    INTEGER NOT NULL REFERENCES principal(id),
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    deleted       BOOLEAN NOT NULL DEFAULT FALSE
);

CREATE TABLE IF NOT EXISTS agent_team_member (
    team_id       UUID NOT NULL REFERENCES agent_team(id) ON DELETE CASCADE,
    agent_id      INTEGER NOT NULL REFERENCES agent(id) ON DELETE CASCADE,
    role          SMALLINT NOT NULL DEFAULT 2, -- 1=LEADER, 2=MEMBER
    responsibility TEXT NOT NULL DEFAULT '',
    joined_at     TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (team_id, agent_id)
);

-- At most one leader per team.
CREATE UNIQUE INDEX IF NOT EXISTS idx_agent_team_member_leader
    ON agent_team_member(team_id) WHERE role = 1;

-- An agent can belong to at most one team.
CREATE UNIQUE INDEX IF NOT EXISTS idx_agent_team_member_agent
    ON agent_team_member(agent_id);

-- A team can be assigned to at most one active (TODO/IN_PROGRESS/IN_REVIEW) task.
ALTER TABLE task ADD COLUMN IF NOT EXISTS assignee_team_id UUID REFERENCES agent_team(id) ON DELETE SET NULL;
CREATE UNIQUE INDEX IF NOT EXISTS idx_task_team_active
    ON task(assignee_team_id) WHERE assignee_team_id IS NOT NULL AND status IN (1,2,3);
