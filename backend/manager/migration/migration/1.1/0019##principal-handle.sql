-- Readable, unique principal handles.
--
-- Users gain a human-readable, immutable mention id ("ran-user-1") stored in
-- principal.handle; agents store their handle directly in agent.resource_id
-- (already text + unique, so no schema change). Mentions and DM addresses are
-- resolved by handle only; display names never participate in resolution.
--
-- Handles are generated at creation time by the store (slugified name + per-
-- type counter). The product is pre-launch and the workspace data was cleared,
-- so the column is added directly as NOT NULL — no backfill is needed.

ALTER TABLE principal ADD COLUMN IF NOT EXISTS handle text NOT NULL;
CREATE UNIQUE INDEX IF NOT EXISTS idx_principal_unique_handle ON principal(handle);
