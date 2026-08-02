-- Add agent owner (authorization authority) superseding created_by (display only).
-- Backfilled from created_by so existing agents keep their current modifier.
-- Version: 1.1.7
ALTER TABLE agent ADD COLUMN IF NOT EXISTS owner_id int NOT NULL DEFAULT 0;
UPDATE agent SET owner_id = created_by WHERE owner_id = 0;
