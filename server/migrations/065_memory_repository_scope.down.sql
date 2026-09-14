DROP INDEX IF EXISTS idx_agent_memories_repo;
DROP INDEX IF EXISTS idx_agent_memories_agent_repo;

ALTER TABLE agent_memories DROP COLUMN IF EXISTS repository_id;

CREATE INDEX IF NOT EXISTS idx_agent_memories_agent_team
    ON agent_memories(agent_id, created_at DESC);
