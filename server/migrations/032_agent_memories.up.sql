CREATE TABLE IF NOT EXISTS agent_memories (
    id         UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    agent_id   UUID NOT NULL REFERENCES agents(id) ON DELETE CASCADE,
    team_id    UUID NOT NULL REFERENCES teams(id) ON DELETE CASCADE,
    content    TEXT NOT NULL,
    category   TEXT NOT NULL DEFAULT '',
    embedding  JSONB,
    source     TEXT NOT NULL DEFAULT 'agent',
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_agent_memories_agent_team
    ON agent_memories(agent_id, team_id, created_at DESC);
