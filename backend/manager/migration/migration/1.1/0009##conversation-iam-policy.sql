-- Conversation membership moves from conversation_member to IAM policies on
-- the policy table (resource_type=CONVERSATION). conversation_member_meta
-- keeps the non-authorization metadata (joined_at, pinning) and serves as the
-- relational read index for membership-filtered list queries.

-- 1. Backfill conversation IAM policies from conversation_member rows. User
-- members become "users/{principal_id}", agent members "agents/{resource_id}";
-- member_role 1/2/3 map to roles/conversationOwner|Member|Admin.
INSERT INTO policy (enforce, updated_at, resource_type, resource, type, payload, inherit_from_parent)
SELECT TRUE, now(), 'CONVERSATION', 'conversations/' || conversation_id::text, 'IAM',
       jsonb_build_object('bindings', jsonb_agg(
           jsonb_build_object('role', role, 'members', members) ORDER BY role
       )),
       FALSE
FROM (
    SELECT conversation_id,
           CASE member_role
               WHEN 1 THEN 'roles/conversationOwner'
               WHEN 3 THEN 'roles/conversationAdmin'
               ELSE 'roles/conversationMember'
           END AS role,
           jsonb_agg(
               CASE member_type
                   WHEN 2 THEN 'agents/' || member_id
                   ELSE 'users/' || member_id
               END ORDER BY member_id
           ) AS members
    FROM conversation_member
    GROUP BY conversation_id, member_role
) grouped
GROUP BY conversation_id
ON CONFLICT (resource_type, resource, type) DO NOTHING;

-- 2. Create the metadata/index table and copy rows (joined_at, pins).
CREATE TABLE conversation_member_meta (
    conversation_id UUID NOT NULL REFERENCES conversation(id) ON DELETE CASCADE,
    member_type SMALLINT NOT NULL,
    member_id TEXT NOT NULL,
    joined_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    pinned BOOLEAN NOT NULL DEFAULT FALSE,
    pinned_at TIMESTAMPTZ,
    PRIMARY KEY (conversation_id, member_type, member_id)
);

INSERT INTO conversation_member_meta (conversation_id, member_type, member_id, joined_at, pinned, pinned_at)
SELECT conversation_id, member_type, member_id, joined_at, pinned, pinned_at
FROM conversation_member;

CREATE INDEX idx_conversation_member_meta_lookup ON conversation_member_meta(member_type, member_id);

-- 3. Drop the old authorization table.
DROP TABLE conversation_member;
