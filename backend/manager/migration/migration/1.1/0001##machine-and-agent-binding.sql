-- Phase 1: introduce the machine entity and bind agents to machines.
--
-- A machine is the long-lived agent-application process a user runs once on a
-- host; it authenticates with a registration token and hosts one or more
-- agents. This migration adds the machine/machine_session/machine_token tables
-- (mirroring the agent_* ones) and a nullable agent.machine_id FK. Per-agent
-- auth/session tables are retired in a later phase once the manager no longer
-- uses them; here we only add the new surface so the change is additive.
--
-- Version: 1.1.1

-- Machine
CREATE TABLE IF NOT EXISTS machine (
    id serial PRIMARY KEY,
    resource_id text NOT NULL,
    name text NOT NULL,
    token_version int NOT NULL DEFAULT 1,
    created_at timestamptz NOT NULL DEFAULT now(),
    deleted boolean NOT NULL DEFAULT FALSE,
    -- Stored as MachineInfo (proto/store/store/machine.proto)
    info jsonb NOT NULL DEFAULT '{}',
    -- Stored as MachineStatus (proto/store/store/machine.proto)
    status jsonb NOT NULL DEFAULT '{}',
    -- Principal id of the user who created the machine (0 = unknown).
    created_by int NOT NULL DEFAULT 0,
    avatar_s3_key text NOT NULL DEFAULT '',
    last_token_rotated_at timestamptz
);

CREATE UNIQUE INDEX IF NOT EXISTS idx_machine_unique_resource_id ON machine(resource_id);
ALTER SEQUENCE machine_id_seq RESTART WITH 101;

-- Machine session (connection liveness, parallel to agent_session)
CREATE TABLE IF NOT EXISTS machine_session (
    id bigserial PRIMARY KEY,
    session_id text NOT NULL UNIQUE,
    machine_id int NOT NULL REFERENCES machine(id) ON DELETE CASCADE,
    token_family text NOT NULL,
    state text NOT NULL DEFAULT 'ACTIVE',
    source_ip text NOT NULL DEFAULT '',
    fingerprint text NOT NULL DEFAULT '',
    agent_version text NOT NULL DEFAULT '',
    connected_at timestamptz NOT NULL DEFAULT now(),
    disconnected_at timestamptz,
    last_heartbeat_at timestamptz NOT NULL DEFAULT now(),
    disconnect_reason text,
    metadata jsonb NOT NULL DEFAULT '{}'
);

CREATE INDEX IF NOT EXISTS idx_machine_session_machine ON machine_session(machine_id, state);
CREATE INDEX IF NOT EXISTS idx_machine_session_session ON machine_session(session_id);
CREATE INDEX IF NOT EXISTS idx_machine_session_active ON machine_session(state, last_heartbeat_at);

-- Machine token (registration/access/refresh, parallel to agent_token)
CREATE TABLE IF NOT EXISTS machine_token (
    id bigserial PRIMARY KEY,
    machine_id int NOT NULL REFERENCES machine(id) ON DELETE CASCADE,
    token_hash text NOT NULL,
    token_type text NOT NULL DEFAULT 'BOOTSTRAP',
    token_family text NOT NULL,
    state text NOT NULL DEFAULT 'ACTIVE',
    fingerprint text NOT NULL DEFAULT '',
    source_ip text NOT NULL DEFAULT '',
    issued_at timestamptz NOT NULL DEFAULT now(),
    expires_at timestamptz NOT NULL,
    consumed_at timestamptz,
    revoked_at timestamptz,
    last_used_at timestamptz,
    created_by text NOT NULL DEFAULT ''
);

CREATE UNIQUE INDEX IF NOT EXISTS idx_machine_token_hash ON machine_token(token_hash);
CREATE INDEX IF NOT EXISTS idx_machine_token_family ON machine_token(token_family, state);
CREATE INDEX IF NOT EXISTS idx_machine_token_machine ON machine_token(machine_id, token_type, state);

-- Bind agents to machines. Nullable here so the migration applies cleanly on a
-- dev DB that may still have legacy agent rows; the application enforces the
-- binding at CreateAgent time. NOT NULL is tightened once legacy rows are gone.
ALTER TABLE agent ADD COLUMN IF NOT EXISTS machine_id INTEGER REFERENCES machine(id) ON DELETE RESTRICT;
CREATE INDEX IF NOT EXISTS idx_agent_machine ON agent(machine_id) WHERE machine_id IS NOT NULL;

-- Denormalized machine_id on command for "fail all commands for a machine"
-- queries. Optional on the hot path (the dispatcher knows the machine from the
-- AgentSession); backfilled from agent.machine_id where known.
ALTER TABLE command ADD COLUMN IF NOT EXISTS machine_id INTEGER REFERENCES machine(id) ON DELETE SET NULL;
CREATE INDEX IF NOT EXISTS idx_command_machine ON command(machine_id) WHERE machine_id IS NOT NULL;
UPDATE command c
SET machine_id = a.machine_id
FROM agent a
WHERE c.agent_id = a.id AND c.machine_id IS NULL AND a.machine_id IS NOT NULL;