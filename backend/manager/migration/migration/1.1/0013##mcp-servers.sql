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
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE UNIQUE INDEX idx_mcp_server_resource_id ON mcp_server(resource_id);

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
