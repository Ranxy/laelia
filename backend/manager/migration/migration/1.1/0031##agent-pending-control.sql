-- agent_pending_control: queued manager→agent control interactions issued
-- while the agent's machine is offline (design "agent command stream
-- resilience" §3.5). CancelCommand marks the command CANCELED immediately (the
-- irreversible anchor) and enqueues the cancel for delivery; SteerCommand
-- enqueues and reports "queued". The machine control stream drains a machine's
-- rows in id order at (re)connect; a row is deleted once delivered or when its
-- target command has reached a terminal state (a cancel does not chase a
-- finished command; a steer is meaningless). Machine deletion cascades via the
-- FK; a TTL sweep (store.PendingControlTTL) reclaims leftovers from
-- permanently retired machines.
CREATE TABLE IF NOT EXISTS agent_pending_control (
    id         serial PRIMARY KEY,
    machine_id int NOT NULL REFERENCES machine(id) ON DELETE CASCADE,
    agent_id   int NOT NULL,
    kind       text NOT NULL,
    command_id uuid NOT NULL,
    text       text NOT NULL DEFAULT '',
    created_at timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_agent_pending_control_machine
    ON agent_pending_control(machine_id, id);

CREATE INDEX IF NOT EXISTS idx_agent_pending_control_created_at
    ON agent_pending_control(created_at);