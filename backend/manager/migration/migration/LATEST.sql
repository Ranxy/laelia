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
    -- handle is the user's human-readable, unique mention id (e.g.
    -- "ran-user-1"), generated at creation and immutable thereafter. The value
    -- typed after "@" to mention or DM the user; the {user} segment of the
    -- users/{handle} resource name.
    handle text NOT NULL,
    password_hash text NOT NULL,
    phone text NOT NULL DEFAULT '',
    -- Stored as MFAConfig (proto/store/store/user.proto)
    mfa_config jsonb NOT NULL DEFAULT '{}',
    -- Stored as UserProfile (proto/store/store/user.proto)
    profile jsonb NOT NULL DEFAULT '{}',
    -- Short, user-authored self-description surfaced to agents/users in channel
    -- and thread rosters so an agent can perceive who a user is and what they focus on.
    description text NOT NULL DEFAULT '',
    -- S3 object key of the user's uploaded avatar image, empty when the user has
    -- not uploaded one (frontend renders a deterministic pixel identicon instead).
    avatar_s3_key text NOT NULL DEFAULT '',
    -- NULL until the user confirms the address via the signup verification
    -- email link; admin-created users and pre-existing users are verified.
    email_verified_at timestamptz
);


CREATE UNIQUE INDEX IF NOT EXISTS idx_principal_unique_handle ON principal(handle);

-- Idempotent ALTER for the chat_preferences column. Nullable: a NULL value
-- means "use the default" (enter_to_send = true, the historic behavior); only
-- an explicit user write persists a real value. Stored as ChatPreferences
-- (proto/store/store/user.proto).
ALTER TABLE principal ADD COLUMN IF NOT EXISTS chat_preferences jsonb;

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
  id text PRIMARY KEY DEFAULT gen_random_uuid()::text,
  email text UNIQUE,
  name text NOT NULL,
  description text NOT NULL DEFAULT '',
  -- Stored as GroupPayload (proto/store/store/group.proto)
  payload jsonb NOT NULL DEFAULT '{}'
);

-- Default system account id is 1.
INSERT INTO principal (id, type, name, email, password_hash, handle) VALUES (1, 'SYSTEM_BOT', 'SYSTEM', 'support@example.com', '', 'system-bot');

ALTER SEQUENCE principal_id_seq RESTART WITH 101;

-- Default project.
INSERT INTO project (id, name, resource_id) VALUES (1, 'Default', 'default');

ALTER SEQUENCE project_id_seq RESTART WITH 101;

-- Agent
CREATE TABLE agent (
    id serial PRIMARY KEY,
    resource_id text NOT NULL,
    name text NOT NULL,
    description text NOT NULL DEFAULT '',
    token_version int NOT NULL DEFAULT 1,
    created_at timestamptz NOT NULL DEFAULT now(),
    deleted boolean NOT NULL DEFAULT FALSE,
    -- Stored as AgentInfo (proto/store/store/agent.proto)
    info jsonb NOT NULL DEFAULT '{}',
    -- Stored as AgentStatus (proto/store/store/agent.proto)
    status jsonb NOT NULL DEFAULT '{}',
    -- Principal id of the user who created the agent (0 = unknown/legacy).
    -- Display-only; never authorizes anything (the agent's owner does).
    created_by int NOT NULL DEFAULT 0,
    -- Principal id of the agent's owner (authorization authority). Backfilled
    -- from created_by on migration; 0 = unknown/legacy (owner unset). Only the
    -- owner or a workspace admin may modify the agent. Transferable via
    -- TransferAgentOwnership.
    owner_id int NOT NULL DEFAULT 0,
    -- allow_add_to_channel: whether other users may add this agent to a channel.
    -- Default FALSE = only the agent's owner or a workspace admin may add it.
    allow_add_to_channel boolean NOT NULL DEFAULT FALSE,
    -- follow_owner_permissions: whether the agent inherits its owner's channel
    -- read access (channels/DMs the owner can read). Default TRUE.
    follow_owner_permissions boolean NOT NULL DEFAULT TRUE,
    -- can_manage_channel_members: whether the agent may add/remove members in a
    -- channel where its owner is a channel Admin/Owner. Default TRUE.
    can_manage_channel_members boolean NOT NULL DEFAULT TRUE,
    -- enabled: whether the agent is running. When false the agent has been
    -- stopped (StopAgent): its machine runner is torn down and it processes no
    -- session messages until StartAgent. Default TRUE.
    enabled boolean NOT NULL DEFAULT TRUE,
    -- S3 object key of the agent's uploaded avatar image, empty when the agent
    -- has not uploaded one (frontend renders a deterministic pixel identicon instead).
    avatar_s3_key text NOT NULL DEFAULT ''
);

CREATE UNIQUE INDEX idx_agent_unique_resource_id ON agent(resource_id);

-- Idempotent ALTER for the agent avatar_s3_key column (fresh installs already get
-- it from CREATE TABLE above).
ALTER TABLE agent ADD COLUMN IF NOT EXISTS avatar_s3_key text NOT NULL DEFAULT '';

-- Public agent description: shown to other users/agents, never injected into the
-- agent's own prompt (persona_prompt in agent.info is the private self prompt).
ALTER TABLE agent ADD COLUMN IF NOT EXISTS description text NOT NULL DEFAULT '';

-- allow_add_to_channel: whether other users may add this agent to a channel.
-- Default FALSE = only the agent's owner or a workspace admin may add it.
ALTER TABLE agent ADD COLUMN IF NOT EXISTS allow_add_to_channel boolean NOT NULL DEFAULT FALSE;

-- follow_owner_permissions: whether the agent inherits its owner's channel
-- read access. Default TRUE.
ALTER TABLE agent ADD COLUMN IF NOT EXISTS follow_owner_permissions boolean NOT NULL DEFAULT TRUE;

-- can_manage_channel_members: whether the agent may add/remove members in a
-- channel where its owner is a channel Admin/Owner. Default TRUE.
ALTER TABLE agent ADD COLUMN IF NOT EXISTS can_manage_channel_members boolean NOT NULL DEFAULT TRUE;

-- enabled: whether the agent is running (stopped agents process no messages).
ALTER TABLE agent ADD COLUMN IF NOT EXISTS enabled boolean NOT NULL DEFAULT TRUE;

-- owner_id: authorization authority for the agent, backfilled from created_by
-- (mirrors the conversation table's created_by -> owner_id migration).
ALTER TABLE agent ADD COLUMN IF NOT EXISTS owner_id int NOT NULL DEFAULT 0;
UPDATE agent SET owner_id = created_by WHERE owner_id = 0;


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
ALTER TABLE agent ADD COLUMN IF NOT EXISTS created_by int NOT NULL DEFAULT 0;

-- Machine
CREATE TABLE machine (
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
    created_by int NOT NULL DEFAULT 0,
    avatar_s3_key text NOT NULL DEFAULT '',
    last_token_rotated_at timestamptz
);

CREATE UNIQUE INDEX idx_machine_unique_resource_id ON machine(resource_id);
ALTER SEQUENCE machine_id_seq RESTART WITH 101;

-- Bind agents to machines (nullable; the application enforces the binding at
-- CreateAgent time).
ALTER TABLE agent ADD COLUMN machine_id INTEGER REFERENCES machine(id) ON DELETE RESTRICT;
CREATE INDEX idx_agent_machine ON agent(machine_id) WHERE machine_id IS NOT NULL;

CREATE TABLE machine_session (
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

CREATE INDEX idx_machine_session_machine ON machine_session(machine_id, state);
CREATE INDEX idx_machine_session_session ON machine_session(session_id);
CREATE INDEX idx_machine_session_active ON machine_session(state, last_heartbeat_at);

CREATE TABLE machine_token (
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

CREATE UNIQUE INDEX idx_machine_token_hash ON machine_token(token_hash);
CREATE INDEX idx_machine_token_family ON machine_token(token_family, state);
CREATE INDEX idx_machine_token_machine ON machine_token(machine_id, token_type, state);

CREATE TABLE audit_log (
    id bigserial PRIMARY KEY,
    method text NOT NULL,
    actor_type text NOT NULL DEFAULT '',
    actor_id text NOT NULL DEFAULT '',
    source_ip text NOT NULL DEFAULT '',
    status text NOT NULL DEFAULT 'ok',
    error text NOT NULL DEFAULT '',
    -- resource is the target resource of the audited call, e.g. "agents/{rid}".
    resource text NOT NULL DEFAULT '',
    -- payload is the structured change payload (e.g. IAM binding deltas).
    payload jsonb NOT NULL DEFAULT '{}',
    created_at timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX idx_audit_log_method ON audit_log(method);
CREATE INDEX idx_audit_log_actor ON audit_log(actor_type, actor_id);
CREATE INDEX idx_audit_log_created_at ON audit_log(created_at);
CREATE INDEX idx_audit_log_resource ON audit_log(resource);

-- Command execution records
CREATE TABLE command (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    agent_id INTEGER NOT NULL REFERENCES agent(id) ON DELETE CASCADE,
    -- Denormalized machine id for "fail all commands for a machine" queries.
    -- Nullable: only session-created commands after the machine refactor set it.
    machine_id INTEGER REFERENCES machine(id) ON DELETE SET NULL,
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
CREATE INDEX idx_command_machine ON command(machine_id) WHERE machine_id IS NOT NULL;

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

-- Per-command token consumption, stored structurally for cheap aggregation.
-- One row per command (command_id UNIQUE): the final token counts reported by
-- the agent runtime at command completion. Dimension columns (agent_id,
-- principal_id, machine_id) are denormalized from command so agent/principal/
-- machine + time aggregates need no join. Writes are idempotent: a replayed
-- TOKEN_USAGE event must not create a duplicate row.
CREATE TABLE IF NOT EXISTS command_token_usage (
    id BIGSERIAL PRIMARY KEY,
    command_id UUID NOT NULL UNIQUE REFERENCES command(id) ON DELETE CASCADE,
    agent_id INTEGER NOT NULL REFERENCES agent(id) ON DELETE CASCADE,
    principal_id INTEGER NOT NULL REFERENCES principal(id),
    machine_id INTEGER REFERENCES machine(id) ON DELETE SET NULL,
    input_tokens BIGINT NOT NULL DEFAULT 0,
    output_tokens BIGINT NOT NULL DEFAULT 0,
    cache_read_tokens BIGINT NOT NULL DEFAULT 0,
    cache_write_tokens BIGINT NOT NULL DEFAULT 0,
    total_tokens BIGINT NOT NULL DEFAULT 0,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_command_token_usage_agent_time
    ON command_token_usage(agent_id, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_command_token_usage_principal_time
    ON command_token_usage(principal_id, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_command_token_usage_machine_time
    ON command_token_usage(machine_id, created_at DESC) WHERE machine_id IS NOT NULL;
CREATE INDEX IF NOT EXISTS idx_command_token_usage_time
    ON command_token_usage(created_at DESC);

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

-- conversation_member_meta is the relational read index and per-user UI
-- metadata (join time, pinning) for conversation membership. Authorization
-- lives in the conversation IAM policy (policy table, resource_type=
-- CONVERSATION); every membership write touches both in one transaction.
CREATE TABLE conversation_member_meta (
    conversation_id UUID NOT NULL REFERENCES conversation(id) ON DELETE CASCADE,
    member_type SMALLINT NOT NULL,
    member_id TEXT NOT NULL,
    joined_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    pinned BOOLEAN NOT NULL DEFAULT FALSE,
    pinned_at TIMESTAMPTZ,
    closed BOOLEAN NOT NULL DEFAULT FALSE,
    closed_at TIMESTAMPTZ,
    PRIMARY KEY (conversation_id, member_type, member_id)
);

-- command_conversation links a drain command to every conversation it touched.
-- A single multi-channel turn may post/ack in several conversations, so this is
-- many-to-many: FetchConversationActivity uses it to show an agent as "running"
-- in each conversation its current command is active in (not just the one
-- command.conversation_id column, which records only the first/primary). The
-- column is retained for the command-detail "primary conversation" view.
CREATE TABLE command_conversation (
    command_id UUID NOT NULL REFERENCES command(id) ON DELETE CASCADE,
    conversation_id UUID NOT NULL REFERENCES conversation(id) ON DELETE CASCADE,
    linked_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (command_id, conversation_id)
);

CREATE INDEX idx_command_conversation_conversation ON command_conversation(conversation_id);

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

-- 3. Drop old unique constraint (channel membership is tracked through the
-- conversation IAM policy + conversation_member_meta index)
DROP INDEX IF EXISTS idx_conversation_agent_principal;

-- 4. Populate conversation_member_meta for existing direct conversations
INSERT INTO conversation_member_meta (conversation_id, member_type, member_id)
SELECT id, 1, created_by::TEXT FROM conversation WHERE type = 1
ON CONFLICT (conversation_id, member_type, member_id) DO NOTHING;

INSERT INTO conversation_member_meta (conversation_id, member_type, member_id)
SELECT c.id, 2, a.resource_id
FROM conversation c
JOIN agent a ON a.id = c.agent_id
WHERE c.type = 1 AND c.agent_id IS NOT NULL
ON CONFLICT (conversation_id, member_type, member_id) DO NOTHING;

-- 5. Add sender_agent_id to chat_message (distinguishes agent-sent messages)
ALTER TABLE chat_message ADD COLUMN IF NOT EXISTS sender_agent_id INTEGER REFERENCES agent(id);

CREATE INDEX IF NOT EXISTS idx_chat_message_sender_agent ON chat_message(sender_agent_id) WHERE sender_agent_id IS NOT NULL;
CREATE INDEX IF NOT EXISTS idx_conversation_member_meta_lookup ON conversation_member_meta(member_type, member_id);

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

-- === User-first: durable per-channel read cursor ===
-- user_channel_cursor records how far a user has read each conversation they
-- are a member of. The frontend compares conversation.version against
-- read_version to render unread badges. A missing row is treated as
-- "caught up to current version" on first read (COALESCE to conversation.version),
-- mirroring agent_channel_cursor semantics, so a newly joined user does not see
-- existing history as unread.
CREATE TABLE IF NOT EXISTS user_channel_cursor (
    principal_id INTEGER NOT NULL REFERENCES principal(id) ON DELETE CASCADE,
    conversation_id UUID NOT NULL REFERENCES conversation(id) ON DELETE CASCADE,
    read_version BIGINT NOT NULL DEFAULT 0,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (principal_id, conversation_id)
);

CREATE INDEX IF NOT EXISTS idx_user_channel_cursor_user ON user_channel_cursor(principal_id);
CREATE INDEX IF NOT EXISTS idx_user_channel_cursor_conv ON user_channel_cursor(conversation_id);

-- The previous Phase 2 held_action table is obsolete in the agent-first model:
-- the agent runs tools and posts replies directly within its own session, and
-- the send-time Held Draft is handled inline by post_message's base_version
-- optimistic-concurrency check. Drop it on upgrade; fresh installs never
-- create it.
DROP TABLE IF EXISTS held_action CASCADE;

-- === S3-backed file attachments ===
-- chat_message.attachments mirrors the mentions JSONB column: a denormalized
-- list of {id,name,mime_type,size_bytes} refs to rows in the file table. Storing
-- the refs inline (rather than a join table) keeps message rendering cheap and
-- matches the existing mentions pattern.
ALTER TABLE chat_message ADD COLUMN IF NOT EXISTS attachments JSONB NOT NULL DEFAULT '[]';

-- file is the persisted metadata for an S3-backed object. Each upload gets a
-- unique uuid even for duplicate original_name values in the same conversation.
-- s3_key is prefixed with the file id so duplicate names never collide in S3.
-- conversation_id is nullable: a file may be uploaded without a conversation
-- (then only the uploader may download it); the channel composer always sets
-- one so membership access control applies.
CREATE TABLE IF NOT EXISTS file (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    conversation_id UUID REFERENCES conversation(id) ON DELETE SET NULL,
    uploader_principal_id INTEGER NOT NULL REFERENCES principal(id),
    original_name TEXT NOT NULL,
    mime_type TEXT NOT NULL DEFAULT '',
    size_bytes BIGINT NOT NULL DEFAULT 0,
    s3_key TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_file_conversation ON file(conversation_id) WHERE conversation_id IS NOT NULL;
CREATE INDEX IF NOT EXISTS idx_file_uploader ON file(uploader_principal_id);


-- === Search: pg_trgm GIN index for leading-wildcard ILIKE ===
-- SearchChatHistory filters chat_message.content with `content ILIKE '%q%'`,
-- a leading-wildcard pattern no btree can serve, forcing a full scan per
-- search. pg_trgm's GIN(gin_trgm_ops) index supports ILIKE and turns that into
-- an index scan. The extension is created idempotently; the index is partial on
-- non-empty content (every chat_message.content is NOT NULL) but guarded with
-- IF NOT EXISTS so re-applying the migration is safe.
CREATE EXTENSION IF NOT EXISTS pg_trgm;
CREATE INDEX IF NOT EXISTS idx_chat_message_content_trgm
    ON chat_message USING GIN (content gin_trgm_ops);

-- === Unique constraints (DM conversation / principal.email / token_hash) ===
-- Three race/correctness gaps closed by unique indexes:
--  1. GetOrCreateDirectConversation did SELECT-then-INSERT; two concurrent
--     callers both observed "no DM" and both inserted. A partial unique index on
--     (agent_id, created_by) for direct conversations (type=1) — channels are
--     type=2 with agent_id NULL and intentionally unconstrained — backs an
--     INSERT ... ON CONFLICT DO NOTHING so only one row wins.
--  2. principal.email had no uniqueness; CreateUser/UpdateUser relied on app-layer
--     lowercasing with no constraint, so duplicate emails could be inserted and
--     GetUserByEmail returned a random one. Unique among non-deleted users so a
--     soft-deleted address can be reused.
--  3. agent_token.token_hash was a non-unique index; GetAgentTokenByHash assumes
--     1:1 hash→row, so a collision silently cross-linked agents. Made unique.
DROP INDEX IF EXISTS idx_agent_token_hash;
CREATE UNIQUE INDEX IF NOT EXISTS idx_agent_token_hash ON agent_token(token_hash);
CREATE UNIQUE INDEX IF NOT EXISTS idx_conversation_dm_unique
    ON conversation(agent_id, created_by) WHERE type = 1;
CREATE UNIQUE INDEX IF NOT EXISTS idx_principal_unique_email
    ON principal(email) WHERE deleted = FALSE;

-- === Threads (sub-conversations rooted at a channel message) ===
-- A thread is rooted at a normal channel message (the root). Replies in the
-- thread are chat_message rows whose thread_root_message_id points at the
-- root; they still belong to the same conversation and share its room_version
-- space (so the existing version/cursor infra keeps working), but the main
-- channel list filters them out (thread_root_message_id IS NULL).
ALTER TABLE chat_message ADD COLUMN IF NOT EXISTS thread_root_message_id UUID REFERENCES chat_message(id) ON DELETE CASCADE;
CREATE INDEX IF NOT EXISTS idx_chat_message_thread_root
    ON chat_message(thread_root_message_id) WHERE thread_root_message_id IS NOT NULL;

-- thread_participant records which agents are subscribed to a thread. An agent
-- is subscribed once it is @mentioned in a thread reply or it posts a reply
-- itself; thereafter every new reply in that thread wakes the agent (even
-- without a fresh @mention). This table is only for agent wake routing;
-- thread access control still uses the conversation IAM policy.
CREATE TABLE IF NOT EXISTS thread_participant (
    thread_root_message_id UUID NOT NULL REFERENCES chat_message(id) ON DELETE CASCADE,
    agent_id INTEGER NOT NULL REFERENCES agent(id) ON DELETE CASCADE,
    joined_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (thread_root_message_id, agent_id)
);

CREATE INDEX IF NOT EXISTS idx_thread_participant_agent ON thread_participant(agent_id);

-- === Tasks (top-level messages with task metadata) ===
-- A task is a top-level channel/DM chat_message with attached metadata: a
-- per-conversation sequence number, a status, and an optional assignee. The
-- chat_message (root) is the source of truth for content/sender/room_version;
-- this row carries the task-specific state. The thread rooted at the chat_message
-- is the task's discussion/approval channel. message_id is both PK and FK, so a
-- task IS its root message and deleting the message cascades to the task.
-- conversation_id is denormalized (already on chat_message) for cheap
-- per-conversation listing without a join.
ALTER TABLE conversation ADD COLUMN IF NOT EXISTS next_task_number INTEGER NOT NULL DEFAULT 1;
COMMENT ON COLUMN conversation.next_task_number IS 'Next per-conversation task number; incremented atomically on task creation';

-- status: 1=TODO, 2=IN_PROGRESS, 3=IN_REVIEW, 4=DONE
CREATE TABLE IF NOT EXISTS task (
    message_id UUID PRIMARY KEY REFERENCES chat_message(id) ON DELETE CASCADE,
    conversation_id UUID NOT NULL REFERENCES conversation(id) ON DELETE CASCADE,
    task_number INTEGER NOT NULL,
    status SMALLINT NOT NULL DEFAULT 1,
    assignee_agent_id INTEGER REFERENCES agent(id) ON DELETE SET NULL,
    -- assignee_type distinguishes the current assignee kind: 1=user, 2=agent
    -- (reuses MemberType semantics). NULL when unassigned.
    assignee_type SMALLINT,
    -- assignee_user_id holds a user assignee (display-only "owner"); the agent
    -- claim flow writes assignee_agent_id instead.
    assignee_user_id INTEGER REFERENCES principal(id) ON DELETE SET NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    completed_at TIMESTAMPTZ,
    CONSTRAINT task_status_check CHECK (status IN (1,2,3,4))
);

CREATE UNIQUE INDEX IF NOT EXISTS idx_task_conversation_number ON task(conversation_id, task_number);
CREATE INDEX IF NOT EXISTS idx_task_conversation_status ON task(conversation_id, status);
CREATE INDEX IF NOT EXISTS idx_task_assignee ON task(assignee_agent_id) WHERE assignee_agent_id IS NOT NULL;

-- === Reminders (scheduled/recurring agent-owned tasks) ===
-- A reminder mirrors the task shape: a top-level chat_message (the trigger
-- message) with attached schedule metadata. message_id is both PK and FK, so a
-- reminder IS its trigger message and deleting the message cascades. The thread
-- rooted at the trigger message is the reminder's discussion channel and where
-- completion/miss system messages are posted. The owning agent (the one that
-- recognized the scheduling intent) claims it at creation, so assignee_agent_id
-- is NOT NULL. status: 1=PENDING, 2=DUE, 3=COMPLETED, 4=CANCELLED, 5=MISSED,
-- 6=FAILED. fire_at is the next fire; cron_expr (NULL = one-shot) + tz drive
-- recurring rescheduling. The retry_* columns record the offline-at-fire
-- backoff attempts so the scheduler's retry process is auditable.
CREATE TABLE IF NOT EXISTS reminder (
    message_id UUID PRIMARY KEY REFERENCES chat_message(id) ON DELETE CASCADE,
    conversation_id UUID NOT NULL REFERENCES conversation(id) ON DELETE CASCADE,
    assignee_agent_id INTEGER NOT NULL REFERENCES agent(id) ON DELETE CASCADE,
    task_content TEXT NOT NULL,
    fire_at TIMESTAMPTZ NOT NULL,
    cron_expr TEXT,
    tz TEXT NOT NULL DEFAULT 'UTC',
    status SMALLINT NOT NULL DEFAULT 1,
    retry_count INTEGER NOT NULL DEFAULT 0,
    next_retry_at TIMESTAMPTZ,
    last_attempt_at TIMESTAMPTZ,
    last_fired_at TIMESTAMPTZ,
    last_completed_at TIMESTAMPTZ,
    result TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT reminder_status_check CHECK (status IN (1,2,3,4,5,6))
);

CREATE INDEX IF NOT EXISTS idx_reminder_assignee_status ON reminder(assignee_agent_id, status);
-- PENDING due scan: the scheduler's 1s tick selects rows whose fire_at has
-- passed. Partial index on status=1 keeps it cheap.
CREATE INDEX IF NOT EXISTS idx_reminder_fire_at ON reminder(fire_at) WHERE status = 1;
-- DUE retry scan: the scheduler's retry tick selects DUE rows whose
-- next_retry_at has passed.
CREATE INDEX IF NOT EXISTS idx_reminder_retry ON reminder(next_retry_at) WHERE status = 2;

-- === Agent-to-agent DM (conversation type 3 = AGENT_DM) ===
-- A type-3 conversation is a private 1:1 DM between exactly two agents (no
-- users). It is owned by the SYSTEM_BOT principal (id=1) so the NOT NULL
-- created_by/owner_id FKs are satisfied; agent-sent messages in it borrow
-- principal_id=1 (see PostMessage's fallback). agent_dm_a/agent_dm_b carry the
-- ordered (a < b) pair of agent.id values for race-free dedup via a partial
-- unique index, mirroring idx_conversation_dm_unique for type-1 user DMs.
-- NULL for type 1/2. type: 1=DM(user+agent), 2=channel, 3=AGENT_DM.
ALTER TABLE conversation ADD COLUMN IF NOT EXISTS agent_dm_a INTEGER REFERENCES agent(id) ON DELETE SET NULL;
ALTER TABLE conversation ADD COLUMN IF NOT EXISTS agent_dm_b INTEGER REFERENCES agent(id) ON DELETE SET NULL;

DO $$
BEGIN
    IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conname = 'conversation_agent_dm_order_check') THEN
        ALTER TABLE conversation ADD CONSTRAINT conversation_agent_dm_order_check
            CHECK (agent_dm_a IS NULL OR agent_dm_b IS NULL OR agent_dm_a < agent_dm_b);
    END IF;
END $$;

CREATE UNIQUE INDEX IF NOT EXISTS idx_conversation_agent_dm_unique
    ON conversation(agent_dm_a, agent_dm_b) WHERE type = 3;

-- === User-to-user DM (conversation type 4 = USER_DM) ===
-- A type-4 conversation is a private 1:1 DM between exactly two users (no
-- agents). The initiator (caller) is the owner of record; both users are
-- conversation policy members. user_dm_a/user_dm_b carry the ordered (a < b)
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

-- Channel titles are unique per channel (type=2) so a "#<title>" address
-- resolves to exactly one conversation. Pre-launch, so no backfill is needed.
CREATE UNIQUE INDEX IF NOT EXISTS idx_conversation_channel_title_unique
    ON conversation(title) WHERE type = 2;

-- === Per-user Activity feed ===
-- activity records, per user, each chat_message relevant to them with the
-- category flags that made it relevant and the per-user read/done state. The
-- row identity is (principal_id, activity_key), NOT the message id:
--   - a MENTION is keyed by the mentioning message_id (each @mention is its own
--     precise pointer — mentions are never folded across messages);
--   - a TASK/REMINDER/THREAD activity is keyed by the thread root, so the root
--     plus every later reply in that thread share ONE row that always points at
--     the latest message (UpsertActivity's ON CONFLICT bumps message_id /
--     room_version / created_at and re-surfaces the row as UNREAD when a newer
--     reply arrives, including resurrecting a Marked-Done row).
-- Categories are bit flags (1=MENTION, 2=TASK, 4=REMINDER, 8=THREAD). State model:
-- UNREAD (read_at NULL, done false) -> READ (read_at set, done false; visible
-- under All) -> DONE (done true; hidden from All/Unread). read_at is advanced by
-- MarkConversationRead when the user's channel cursor passes the row's
-- room_version. thread_root_message_id is the thread root for folded rows and
-- for mentions inside a thread; NULL for top-level mentions.
CREATE TABLE IF NOT EXISTS activity (
    principal_id INTEGER NOT NULL REFERENCES principal(id) ON DELETE CASCADE,
    activity_key UUID NOT NULL,
    message_id UUID NOT NULL REFERENCES chat_message(id) ON DELETE CASCADE,
    conversation_id UUID NOT NULL REFERENCES conversation(id) ON DELETE CASCADE,
    thread_root_message_id UUID,
    categories INTEGER NOT NULL DEFAULT 0,
    room_version BIGINT NOT NULL,
    read_at TIMESTAMPTZ,
    done BOOLEAN NOT NULL DEFAULT FALSE,
    done_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (principal_id, activity_key)
);

CREATE INDEX IF NOT EXISTS idx_activity_user_state_created
    ON activity (principal_id, done, read_at, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_activity_user_conv_version
    ON activity (principal_id, conversation_id, room_version);

-- user_thread_participant mirrors thread_participant (agent-only) for users. A
-- user is subscribed to a thread when they are @mentioned in it or they post a
-- reply in it; thereafter every new reply in that thread generates a THREAD
-- activity for that user. Access control still uses the conversation IAM policy.
CREATE TABLE IF NOT EXISTS user_thread_participant (
    thread_root_message_id UUID NOT NULL REFERENCES chat_message(id) ON DELETE CASCADE,
    principal_id INTEGER NOT NULL REFERENCES principal(id) ON DELETE CASCADE,
    joined_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (thread_root_message_id, principal_id)
);

CREATE INDEX IF NOT EXISTS idx_user_thread_participant_user
    ON user_thread_participant (principal_id);

-- GIN index on chat_message.mentions for mention-driven activity queries
-- (finding all messages that mention a given user id). Generation writes
-- activity inline, so this is mainly for any future backfill/debug.
CREATE INDEX IF NOT EXISTS idx_chat_message_mentions_gin
    ON chat_message USING GIN (mentions jsonb_path_ops);

-- web_push_subscription stores per-user browser Web Push endpoints so the
-- manager can deliver system notifications for directed messages even when the
-- user's tab is closed. One user may have many subscriptions (multiple
-- devices/browsers). PK (principal_id, endpoint) makes re-subscribing the same
-- browser idempotent; ON DELETE CASCADE drops a user's subscriptions with the
-- account. Keys (p256dh, auth) are refreshed on upsert since browsers can
-- rotate them.
CREATE TABLE IF NOT EXISTS web_push_subscription (
    principal_id INTEGER NOT NULL REFERENCES principal(id) ON DELETE CASCADE,
    endpoint     TEXT NOT NULL,
    p256dh       TEXT NOT NULL,
    auth         TEXT NOT NULL,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (principal_id, endpoint)
);

CREATE INDEX IF NOT EXISTS idx_web_push_subscription_user
    ON web_push_subscription (principal_id);

-- Schema migration history (bytebase-style version tracking). The migrator
-- records each applied schema version here; the UNIQUE(version) index guards
-- against double-application. Created by LATEST.sql on fresh installs.
CREATE TABLE IF NOT EXISTS schema_migration_history (
    id bigserial PRIMARY KEY,
    version text NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now()
);
CREATE UNIQUE INDEX IF NOT EXISTS idx_schema_migration_history_unique_version
    ON schema_migration_history (version);

-- Global LLM API provider management. A provider bundles (api_key, model)
-- entries plus the users/groups allowed to use them. Agents reference a
-- provider entry via AgentACPConfig.global_provider/global_provider_entry; the
-- api key is resolved server-side at the daemon boundary and never returned by
-- the v1 API (entries expose only a masked form).
CREATE TABLE api_provider (
    id serial PRIMARY KEY,
    resource_id text NOT NULL,
    name text NOT NULL,
    provider_type text NOT NULL,
    base_url text NOT NULL DEFAULT '',
    description text NOT NULL DEFAULT '',
    created_by int NOT NULL DEFAULT 0,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now()
);
CREATE UNIQUE INDEX idx_api_provider_resource_id ON api_provider(resource_id);

CREATE TABLE api_provider_entry (
    id serial PRIMARY KEY,
    provider_id int NOT NULL REFERENCES api_provider(id) ON DELETE CASCADE,
    label text NOT NULL DEFAULT '',
    model_name text NOT NULL,
    -- Plaintext-at-rest (consistent with the S3 secret and the legacy per-agent
    -- api_key posture); masked on read. Encryption-at-rest is a future
    -- enhancement pending a key-management primitive.
    api_key text NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX idx_api_provider_entry_provider ON api_provider_entry(provider_id);

CREATE TABLE api_provider_member (
    provider_id int NOT NULL REFERENCES api_provider(id) ON DELETE CASCADE,
    member text NOT NULL,
    PRIMARY KEY (provider_id, member)
);

-- Workspace-global, admin-managed MCP service registry. The manager holds the
-- transport config (URL + header values, plaintext-at-rest like api_provider
-- keys; masked on read) and only exposes a per-agent tool catalog to machines.
CREATE TABLE mcp_server (
    id BIGSERIAL PRIMARY KEY,
    resource_id TEXT NOT NULL,
    title TEXT NOT NULL,
    description TEXT NOT NULL DEFAULT '',
    transport_type TEXT NOT NULL,
    url TEXT NOT NULL,
    headers JSONB NOT NULL DEFAULT '{}',
    config_version BIGINT NOT NULL DEFAULT 1,
    created_by BIGINT NOT NULL DEFAULT 0,
    owner_id BIGINT NOT NULL DEFAULT 0,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE UNIQUE INDEX idx_mcp_server_resource_id ON mcp_server(resource_id);
CREATE INDEX idx_mcp_server_owner_id ON mcp_server(owner_id);

CREATE TABLE mcp_server_member (
    server_id BIGINT NOT NULL REFERENCES mcp_server(id) ON DELETE CASCADE,
    member TEXT NOT NULL,
    PRIMARY KEY (server_id, member)
);

-- agent_mcp records which MCP servers an agent has enabled. assignment_version
-- bumps on every replace so the gateway can reject stale tool catalogs.
CREATE TABLE agent_mcp (
    agent_id BIGINT NOT NULL REFERENCES agent(id) ON DELETE CASCADE,
    mcp_server_id BIGINT NOT NULL REFERENCES mcp_server(id) ON DELETE RESTRICT,
    assignment_version BIGINT NOT NULL DEFAULT 1,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (agent_id, mcp_server_id)
);

-- === Email verification (self-service signup) ===
-- NULL email_verified_at marks an unverified account created by self-service
-- signup with email verification required; such accounts cannot sign in until
-- the verification link is clicked. Every pre-existing and admin-created
-- account is backfilled as verified so only new self-signups are affected.
ALTER TABLE principal ADD COLUMN IF NOT EXISTS email_verified_at timestamptz;
UPDATE principal SET email_verified_at = now() WHERE email_verified_at IS NULL;

-- Single-use email verification tokens. Only the SHA-256 hash of the token is
-- stored (aligned with agent_token), so a database leak cannot be used to
-- verify arbitrary accounts.
CREATE TABLE IF NOT EXISTS email_verification_token (
    id bigserial PRIMARY KEY,
    token_hash text NOT NULL UNIQUE,
    principal_id int NOT NULL REFERENCES principal(id) ON DELETE CASCADE,
    expires_at timestamptz NOT NULL,
    consumed_at timestamptz,
    created_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS idx_email_verification_token_principal
    ON email_verification_token(principal_id);

-- message_reaction: message emoji reactions (lightweight feedback, sideband).
-- Never bumps the conversation room version, wakes agents, counts as unread,
-- or generates activity. Actor is exactly one of principal_id (user) or
-- agent_id (agent), so both actor columns stay nullable. "One row per
-- (message, actor, emoji)" is enforced by two partial UNIQUE indexes (PK
-- columns are implicitly NOT NULL in Postgres and would reject the NULL actor
-- column), which make adds idempotent (ON CONFLICT DO NOTHING); removes are
-- naturally idempotent.
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
