-- command.failure_kind: why a command is FAILED (empty/NULL otherwise).
-- 'machine_unreachable': the manager reaped the command after its machine was
--   lost past the disconnect grace (dual signal: MachineChannel unregistered +
--   persisted heartbeat expired). These rows are re-gradable: a late terminal
--   from the machine can flip the command to COMPLETED (with a SYSTEM
--   explanation event) or re-attribute the failure to agent_failed.
-- 'agent_failed': the machine reported the failure itself (or a late result
--   re-attributed a machine_unreachable reap). Never re-graded.
ALTER TABLE command ADD COLUMN IF NOT EXISTS failure_kind TEXT;

COMMENT ON COLUMN command.failure_kind IS 'Why the command failed: machine_unreachable (manager reap after disconnect grace, re-gradable) or agent_failed (the machine''s own failure terminal). Empty unless FAILED.';