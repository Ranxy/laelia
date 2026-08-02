-- audit_log gains resource and payload columns so IAM set operations can
-- record the target resource and the binding deltas they applied.
ALTER TABLE audit_log ADD COLUMN IF NOT EXISTS resource text NOT NULL DEFAULT '';
ALTER TABLE audit_log ADD COLUMN IF NOT EXISTS payload jsonb NOT NULL DEFAULT '{}';

CREATE INDEX IF NOT EXISTS idx_audit_log_resource ON audit_log(resource);
