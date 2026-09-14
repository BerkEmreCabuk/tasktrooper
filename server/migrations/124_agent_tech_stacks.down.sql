-- See the matching comment in the .up.sql: DROP CONSTRAINT and DROP COLUMN
-- touch a table under FORCE ROW LEVEL SECURITY, and a migration has no tenant
-- in it to satisfy current_setting('app.tenant_id'). Local to this
-- transaction, nil uuid, exactly as migration 121.
SELECT set_config('app.tenant_id', '00000000-0000-0000-0000-000000000000', true);

DROP INDEX IF EXISTS idx_skills_tech_stack;
ALTER TABLE skills DROP CONSTRAINT IF EXISTS skills_tech_stack_id_fkey;
ALTER TABLE skills DROP COLUMN IF EXISTS tech_stack_id;

DROP TABLE IF EXISTS agent_tech_stacks;
