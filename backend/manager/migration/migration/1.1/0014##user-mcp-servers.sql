-- Personal (user-owned) MCP servers. owner_id = 0 means workspace-global
-- (admin-managed); owner_id > 0 means the server is private to that user and
-- usable only on agents owned by the same user.
ALTER TABLE mcp_server ADD COLUMN owner_id BIGINT NOT NULL DEFAULT 0;
CREATE INDEX idx_mcp_server_owner_id ON mcp_server(owner_id);
