-- idp stores generic identity provider.
CREATE TABLE idp (
  id serial PRIMARY KEY,
  resource_id text NOT NULL,
  name text NOT NULL,
  domain text NOT NULL,
  type text NOT NULL CONSTRAINT idp_type_check CHECK (type IN ('OAUTH2', 'OIDC', 'LDAP')),
  -- config stores the corresponding configuration of the IdP, which may vary depending on the type of the IdP.
  -- Stored as IdentityProviderConfig (proto/store/store/idp.proto)
  config jsonb NOT NULL DEFAULT '{}'
);

CREATE UNIQUE INDEX idx_idp_unique_resource_id ON idp(resource_id);

ALTER SEQUENCE idp_id_seq RESTART WITH 101;

-- principal
CREATE TABLE principal (
    id serial PRIMARY KEY,
    deleted boolean NOT NULL DEFAULT FALSE,
    created_at timestamptz NOT NULL DEFAULT now(),
    type text NOT NULL CHECK (type IN ('END_USER', 'SYSTEM_BOT', 'SERVICE_ACCOUNT')),
    name text NOT NULL,
    email text NOT NULL,
    password_hash text NOT NULL,
    phone text NOT NULL DEFAULT '',
    -- Stored as MFAConfig (proto/store/store/user.proto)
    mfa_config jsonb NOT NULL DEFAULT '{}',
    -- Stored as UserProfile (proto/store/store/user.proto)
    profile jsonb NOT NULL DEFAULT '{}'
);

-- Setting
CREATE TABLE setting (
    id serial PRIMARY KEY,
    -- name: AUTH_SECRET, BRANDING_LOGO, WORKSPACE_ID, WORKSPACE_PROFILE, WORKSPACE_APPROVAL,
    -- WORKSPACE_EXTERNAL_APPROVAL, APP_IM, WATERMARK, AI,
    -- DATA_CLASSIFICATION, SEMANTIC_TYPES, SCIM, PASSWORD_RESTRICTION, ENVIRONMENT
    -- Enum: SettingName (proto/store/store/setting.proto)
    name text NOT NULL,
    value text NOT NULL
);

CREATE UNIQUE INDEX idx_setting_unique_name ON setting(name);

ALTER SEQUENCE setting_id_seq RESTART WITH 101;


-- Role
CREATE TABLE role (
    id bigserial PRIMARY KEY,
    resource_id text NOT NULL,
    name text NOT NULL,
    description text NOT NULL,
    -- Stored as RolePermissions (proto/store/store/role.proto)
    permissions jsonb NOT NULL DEFAULT '{}',
    -- saved for future use
    payload jsonb NOT NULL DEFAULT '{}'
);

CREATE UNIQUE INDEX idx_role_unique_resource_id on role (resource_id);

ALTER SEQUENCE role_id_seq RESTART WITH 101;


-- Policy
-- policy stores the policies for each resources.
CREATE TABLE policy (
    id serial PRIMARY KEY,
    enforce boolean NOT NULL DEFAULT TRUE,
    updated_at timestamptz NOT NULL DEFAULT now(),
    -- resource_type: WORKSPACE, ENVIRONMENT, PROJECT
    -- Enum: Policy.Resource (proto/store/store/policy.proto)
    resource_type text NOT NULL,
    -- resource: resource name in format like "environments/{environment}", "projects/{project}", etc.
    resource TEXT NOT NULL,
    -- Enum: Policy.Type (proto/store/store/policy.proto)
    type text NOT NULL,
    -- Stored as different types based on policy type (proto/store/store/policy.proto):
    payload jsonb NOT NULL DEFAULT '{}',
    inherit_from_parent boolean NOT NULL DEFAULT TRUE
);

CREATE UNIQUE INDEX idx_policy_unique_resource_type_resource_type ON policy(resource_type, resource, type);

ALTER SEQUENCE policy_id_seq RESTART WITH 101;

-- Project
CREATE TABLE project (
    id serial PRIMARY KEY,
    deleted boolean NOT NULL DEFAULT FALSE,
    name text NOT NULL,
    resource_id text NOT NULL,
    data_classification_config_id text NOT NULL DEFAULT '',
    -- Stored as Project (proto/store/store/project.proto)
    setting jsonb NOT NULL DEFAULT '{}'
);

CREATE UNIQUE INDEX idx_project_unique_resource_id ON project(resource_id);


CREATE TABLE user_group (
  email text PRIMARY KEY,
  name text NOT NULL,
  description text NOT NULL DEFAULT '',
  -- Stored as GroupPayload (proto/store/store/group.proto)
  payload jsonb NOT NULL DEFAULT '{}'
);

-- Default system account id is 1.
INSERT INTO principal (id, type, name, email, password_hash) VALUES (1, 'SYSTEM_BOT', 'SYSTEM', 'support@example.com', '');

ALTER SEQUENCE principal_id_seq RESTART WITH 101;

-- Default project.
INSERT INTO project (id, name, resource_id) VALUES (1, 'Default', 'default');

ALTER SEQUENCE project_id_seq RESTART WITH 101;

-- Agent
CREATE TABLE agent (
    id serial PRIMARY KEY,
    resource_id text NOT NULL,
    name text NOT NULL,
    token_version int NOT NULL DEFAULT 1,
    created_at timestamptz NOT NULL DEFAULT now(),
    deleted boolean NOT NULL DEFAULT FALSE,
    -- Stored as AgentInfo (proto/store/store/agent.proto)
    info jsonb NOT NULL DEFAULT '{}',
    -- Stored as AgentStatus (proto/store/store/agent.proto)
    status jsonb NOT NULL DEFAULT '{}'
);

CREATE UNIQUE INDEX idx_agent_unique_resource_id ON agent(resource_id);

ALTER SEQUENCE agent_id_seq RESTART WITH 101;

CREATE TABLE agent_session (
    id bigserial PRIMARY KEY,
    session_id text NOT NULL UNIQUE,
    agent_id int NOT NULL REFERENCES agent(id) ON DELETE CASCADE,
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

CREATE INDEX idx_agent_session_agent ON agent_session(agent_id, state);
CREATE INDEX idx_agent_session_session ON agent_session(session_id);
CREATE INDEX idx_agent_session_active ON agent_session(state, last_heartbeat_at);

CREATE TABLE agent_token (
    id bigserial PRIMARY KEY,
    agent_id int NOT NULL REFERENCES agent(id) ON DELETE CASCADE,
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

CREATE INDEX idx_agent_token_hash ON agent_token(token_hash);
CREATE INDEX idx_agent_token_family ON agent_token(token_family, state);
CREATE INDEX idx_agent_token_agent ON agent_token(agent_id, token_type, state);

ALTER TABLE agent ADD COLUMN last_token_rotated_at timestamptz;

CREATE TABLE audit_log (
    id bigserial PRIMARY KEY,
    method text NOT NULL,
    actor_type text NOT NULL DEFAULT '',
    actor_id text NOT NULL DEFAULT '',
    source_ip text NOT NULL DEFAULT '',
    status text NOT NULL DEFAULT 'ok',
    error text NOT NULL DEFAULT '',
    created_at timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX idx_audit_log_method ON audit_log(method);
CREATE INDEX idx_audit_log_actor ON audit_log(actor_type, actor_id);
CREATE INDEX idx_audit_log_created_at ON audit_log(created_at);

-- Command execution records
CREATE TABLE command (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    agent_id INTEGER NOT NULL REFERENCES agent(id) ON DELETE CASCADE,
    principal_id INTEGER NOT NULL REFERENCES principal(id),
    command TEXT NOT NULL,
    instruction TEXT NOT NULL DEFAULT '',
    profile TEXT NOT NULL DEFAULT '',
    allow_diff BOOLEAN NOT NULL DEFAULT FALSE,
    -- status: 1=PENDING, 2=RUNNING, 3=COMPLETED, 4=FAILED, 5=CANCELLED, 6=TIMEOUT
    status SMALLINT NOT NULL DEFAULT 1,
    exit_code INTEGER,
    duration_ms BIGINT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    started_at TIMESTAMPTZ,
    completed_at TIMESTAMPTZ,
    -- Stored as CommandResult proto
    result_json JSONB NOT NULL DEFAULT '{}',
    env JSONB NOT NULL DEFAULT '{}',
    working_dir TEXT NOT NULL DEFAULT '',
    timeout_seconds INTEGER NOT NULL DEFAULT 0,
    error_message TEXT NOT NULL DEFAULT '',
    final_summary TEXT NOT NULL DEFAULT '',
    last_ack_seq INTEGER NOT NULL DEFAULT 0
);

CREATE INDEX idx_command_agent_status ON command(agent_id, status);
CREATE INDEX idx_command_created_at ON command(created_at DESC);
CREATE INDEX idx_command_agent_pending ON command(agent_id, created_at) WHERE status = 1;

-- Real-time output chunks (streaming progress)
CREATE TABLE command_output (
    id BIGSERIAL PRIMARY KEY,
    command_id UUID NOT NULL REFERENCES command(id) ON DELETE CASCADE,
    seq_no INTEGER NOT NULL,
    -- stream_type: 1=stdout, 2=stderr, 3=system
    stream_type SMALLINT NOT NULL,
    content TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE UNIQUE INDEX idx_command_output_seq ON command_output(command_id, seq_no);

CREATE TABLE command_event (
    id BIGSERIAL PRIMARY KEY,
    command_id UUID NOT NULL REFERENCES command(id) ON DELETE CASCADE,
    seq_no INTEGER NOT NULL,
    event_type SMALLINT NOT NULL,
    summary TEXT NOT NULL DEFAULT '',
    payload_json JSONB NOT NULL DEFAULT '{}',
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE UNIQUE INDEX idx_command_event_seq ON command_event(command_id, seq_no);
CREATE INDEX idx_command_event_created_at ON command_event(command_id, created_at);

ALTER TABLE command ADD COLUMN conversation_id UUID;
CREATE INDEX idx_command_chat_history ON command(agent_id, principal_id, created_at DESC) WHERE conversation_id IS NOT NULL;

CREATE TABLE conversation (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    agent_id INTEGER NOT NULL REFERENCES agent(id),
    title TEXT NOT NULL DEFAULT '',
    type SMALLINT NOT NULL DEFAULT 1,
    created_by INTEGER NOT NULL REFERENCES principal(id),
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE UNIQUE INDEX idx_conversation_agent_principal ON conversation(agent_id, created_by, type);

CREATE TABLE conversation_member (
    conversation_id UUID NOT NULL REFERENCES conversation(id) ON DELETE CASCADE,
    member_type SMALLINT NOT NULL,
    member_id TEXT NOT NULL,
    PRIMARY KEY (conversation_id, member_type, member_id)
);

CREATE TABLE chat_message (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    conversation_id UUID NOT NULL REFERENCES conversation(id) ON DELETE CASCADE,
    principal_id INTEGER NOT NULL REFERENCES principal(id),
    role SMALLINT NOT NULL DEFAULT 1,
    content TEXT NOT NULL,
    command_id UUID REFERENCES command(id) ON DELETE SET NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_chat_message_conversation ON chat_message(conversation_id, created_at);
CREATE INDEX idx_chat_message_command ON chat_message(command_id) WHERE command_id IS NOT NULL;

-- === Channel/Unified Conversation Model Migration ===

-- 1. Make conversation.agent_id nullable (channels don't belong to a single agent)
ALTER TABLE conversation ALTER COLUMN agent_id DROP NOT NULL;

-- 2. Add owner_id and updated_at to conversation
ALTER TABLE conversation ADD COLUMN IF NOT EXISTS owner_id INTEGER REFERENCES principal(id);
ALTER TABLE conversation ADD COLUMN IF NOT EXISTS updated_at TIMESTAMPTZ NOT NULL DEFAULT now();

-- Migrate existing rows: owner_id = created_by
UPDATE conversation SET owner_id = created_by WHERE owner_id IS NULL;
ALTER TABLE conversation ALTER COLUMN owner_id SET NOT NULL;

-- 3. Drop old unique constraint (channel membership is tracked through conversation_member)
DROP INDEX IF EXISTS idx_conversation_agent_principal;

-- 4. Populate conversation_member for existing direct conversations
INSERT INTO conversation_member (conversation_id, member_type, member_id)
SELECT id, 1, created_by::TEXT FROM conversation WHERE type = 1
ON CONFLICT (conversation_id, member_type, member_id) DO NOTHING;

INSERT INTO conversation_member (conversation_id, member_type, member_id)
SELECT c.id, 2, a.resource_id
FROM conversation c
JOIN agent a ON a.id = c.agent_id
WHERE c.type = 1 AND c.agent_id IS NOT NULL
ON CONFLICT (conversation_id, member_type, member_id) DO NOTHING;

-- 5. Add sender_agent_id to chat_message (distinguishes agent-sent messages)
ALTER TABLE chat_message ADD COLUMN IF NOT EXISTS sender_agent_id INTEGER REFERENCES agent(id);

-- 6. Add joined_at and member_role to conversation_member
ALTER TABLE conversation_member ADD COLUMN IF NOT EXISTS joined_at TIMESTAMPTZ NOT NULL DEFAULT now();
ALTER TABLE conversation_member ADD COLUMN IF NOT EXISTS member_role SMALLINT NOT NULL DEFAULT 2;

CREATE INDEX IF NOT EXISTS idx_chat_message_sender_agent ON chat_message(sender_agent_id) WHERE sender_agent_id IS NOT NULL;
CREATE INDEX IF NOT EXISTS idx_conversation_member_lookup ON conversation_member(member_type, member_id);

-- === Phase 1: Message-Driven Architecture ===
-- Room version control: conversation.version increments on every new
-- chat_message and is the basis for each agent's durable per-channel cursor
-- (agent_channel_cursor) and the post_message Held Draft base_version check.
ALTER TABLE conversation ADD COLUMN IF NOT EXISTS version BIGINT NOT NULL DEFAULT 1;
COMMENT ON COLUMN conversation.version IS 'Room version; increments on every new chat_message';

-- chat_message records the room_version at creation time and the sender_type.
-- sender_type: 1=USER, 2=AGENT, 3=SYSTEM (replaces the deprecated
-- CommandSource enum at the message layer).
ALTER TABLE chat_message ADD COLUMN IF NOT EXISTS room_version BIGINT NOT NULL DEFAULT 0;
ALTER TABLE chat_message ADD COLUMN IF NOT EXISTS sender_type SMALLINT NOT NULL DEFAULT 1;
COMMENT ON COLUMN chat_message.room_version IS 'conversation.version at message creation';
COMMENT ON COLUMN chat_message.sender_type IS '1=USER, 2=AGENT, 3=SYSTEM';

-- Backfill sender_type from existing rows. System bot (principal_id=1) user
-- messages are treated as SYSTEM; assistant role with a sender agent is
-- AGENT; everything else user-authored is USER.
UPDATE chat_message
   SET sender_type = 2
 WHERE role = 2 AND sender_agent_id IS NOT NULL AND sender_type = 1;
UPDATE chat_message
   SET sender_type = 3
 WHERE role = 1 AND principal_id = 1 AND sender_type = 1;

CREATE INDEX IF NOT EXISTS idx_chat_message_room_version ON chat_message(conversation_id, room_version);

ALTER TABLE chat_message ADD COLUMN IF NOT EXISTS mentions JSONB NOT NULL DEFAULT '[]';

-- Phase 3: drop the deprecated executor_kind and source_type columns.
-- All commands now execute via ACP and originate from chat messages.
ALTER TABLE command DROP COLUMN IF EXISTS executor_kind;
ALTER TABLE command DROP COLUMN IF EXISTS source_type;

-- Drop the Phase 1 inbox model. Drop IF EXISTS also covers fresh installs
-- (the CREATE TABLE statements previously here have been removed so fresh
-- installs never create these tables, while upgrades from the inbox-era
-- schema drop them deterministically).
DROP TABLE IF EXISTS agent_working_state CASCADE;
DROP TABLE IF EXISTS agent_inbox CASCADE;

-- === Agent-first: durable per-channel cursor ===
-- agent_channel_cursor records how far an agent has processed each
-- conversation it is a member of. The autonomous drain loop compares
-- conversation.version against processed_version to decide whether a channel
-- has unread messages. A missing row is treated as "caught up to current
-- version" on first read (backfill-on-read), so newly joined agents see only
-- future messages unless they fetch history explicitly.
CREATE TABLE IF NOT EXISTS agent_channel_cursor (
    agent_id INTEGER NOT NULL REFERENCES agent(id) ON DELETE CASCADE,
    conversation_id UUID NOT NULL REFERENCES conversation(id) ON DELETE CASCADE,
    processed_version BIGINT NOT NULL DEFAULT 0,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (agent_id, conversation_id)
);

CREATE INDEX IF NOT EXISTS idx_agent_channel_cursor_agent ON agent_channel_cursor(agent_id);
CREATE INDEX IF NOT EXISTS idx_agent_channel_cursor_conv ON agent_channel_cursor(conversation_id);

-- The previous Phase 2 held_action table is obsolete in the agent-first model:
-- the agent runs tools and posts replies directly within its own session, and
-- the send-time Held Draft is handled inline by post_message's base_version
-- optimistic-concurrency check. Drop it on upgrade; fresh installs never
-- create it.
DROP TABLE IF EXISTS held_action CASCADE;

