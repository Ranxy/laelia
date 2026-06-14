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

