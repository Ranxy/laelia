-- Provisioner: management-plane worker that creates machine workloads in one
-- virtualization backend (kubernetes, docker, ...) on the manager's behalf.
CREATE TABLE IF NOT EXISTS provisioner (
    id serial PRIMARY KEY,
    resource_id text NOT NULL,
    name text NOT NULL,
    backend text NOT NULL,
    description text NOT NULL DEFAULT '',
    token_version int NOT NULL DEFAULT 1,
    created_by int NOT NULL DEFAULT 0,
    deleted boolean NOT NULL DEFAULT FALSE,
    created_at timestamptz NOT NULL DEFAULT now(),
    -- Stored as ProvisionerStatus (proto/store/store/provisioner.proto)
    status jsonb NOT NULL DEFAULT '{}'
);

CREATE UNIQUE INDEX IF NOT EXISTS idx_provisioner_unique_resource_id ON provisioner(resource_id);

-- Machine <-> provisioner binding. A machine created via a provisioner keeps
-- the link for its whole life so DeleteMachine can deprovision the workload;
-- provisioning carries the job state (store ProvisioningStatus). Replay of
-- in-flight jobs on provisioner reconnect is a provisioner_id-bounded lookup
-- (machines per provisioner are few); phases are filtered in Go, so no
-- phase-partial index is needed.
ALTER TABLE machine ADD COLUMN IF NOT EXISTS provisioner_id int REFERENCES provisioner(id);
ALTER TABLE machine ADD COLUMN IF NOT EXISTS provisioning jsonb;
CREATE INDEX IF NOT EXISTS idx_machine_provisioner ON machine(provisioner_id) WHERE provisioner_id IS NOT NULL;