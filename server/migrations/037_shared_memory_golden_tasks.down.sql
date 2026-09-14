DROP TABLE IF EXISTS agent_golden_results;
DROP TABLE IF EXISTS agent_golden_tasks;
DELETE FROM agent_memories WHERE agent_id IS NULL;
ALTER TABLE agent_memories ALTER COLUMN agent_id SET NOT NULL;
