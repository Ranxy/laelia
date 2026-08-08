-- Per-command token consumption, stored structurally for cheap aggregation.
--
-- One row per command (command_id UNIQUE): the final token counts reported by
-- the agent runtime at command completion. Dimension columns (agent_id,
-- principal_id, machine_id) are denormalized from command so agent/principal/
-- machine + time aggregates need no join. Writes are idempotent: a replayed
-- TOKEN_USAGE event must not create a duplicate row.
--
-- Version: 1.1.16

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

-- Aggregation indexes: dimension + time bucket.
CREATE INDEX IF NOT EXISTS idx_command_token_usage_agent_time
    ON command_token_usage(agent_id, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_command_token_usage_principal_time
    ON command_token_usage(principal_id, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_command_token_usage_machine_time
    ON command_token_usage(machine_id, created_at DESC) WHERE machine_id IS NOT NULL;
-- Global time-range queries.
CREATE INDEX IF NOT EXISTS idx_command_token_usage_time
    ON command_token_usage(created_at DESC);
