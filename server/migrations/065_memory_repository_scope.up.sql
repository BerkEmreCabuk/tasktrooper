-- Project-scoped memory: repository_id NULL means the memory is global (valid
-- everywhere), a value means it only applies while working in that repository.
-- Combined with the existing agent_id NULL = team convention this gives four
-- buckets: agent/global, agent/project, team/global, team/project.
ALTER TABLE agent_memories
    ADD COLUMN IF NOT EXISTS repository_id UUID REFERENCES repositories(id) ON DELETE CASCADE;

DROP INDEX IF EXISTS idx_agent_memories_agent_team;

CREATE INDEX IF NOT EXISTS idx_agent_memories_agent_repo
    ON agent_memories(agent_id, repository_id, created_at DESC);

CREATE INDEX IF NOT EXISTS idx_agent_memories_repo
    ON agent_memories(repository_id, created_at DESC);
