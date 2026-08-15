-- Task assignee can now be a user (display-only "owner") in addition to an
-- agent. assignee_type distinguishes the current assignee kind (1=user,
-- 2=agent, reusing MemberType semantics); assignee_user_id holds the user
-- assignee. The agent claim flow still writes assignee_agent_id (with
-- assignee_type=2); a user assignee is display-only and does not participate
-- in claim/process flows.
-- Version: 1.1.22
ALTER TABLE task ADD COLUMN IF NOT EXISTS assignee_type SMALLINT;
ALTER TABLE task ADD COLUMN IF NOT EXISTS assignee_user_id INTEGER REFERENCES principal(id) ON DELETE SET NULL;
