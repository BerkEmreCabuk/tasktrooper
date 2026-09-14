DROP INDEX IF EXISTS idx_agent_memories_owner;
DROP INDEX IF EXISTS idx_board_tasks_assignee_user;
DROP INDEX IF EXISTS idx_agents_owner;

ALTER TABLE agent_memories DROP COLUMN IF EXISTS owner_user_id;
ALTER TABLE board_tasks DROP COLUMN IF EXISTS assignee_user_id;
ALTER TABLE agents DROP COLUMN IF EXISTS owner_user_id;

DROP POLICY IF EXISTS tenant_isolation ON tenant_members;
DROP TABLE IF EXISTS tenant_members;
DROP TABLE IF EXISTS tenants;
