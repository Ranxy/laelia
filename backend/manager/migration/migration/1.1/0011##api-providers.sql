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
