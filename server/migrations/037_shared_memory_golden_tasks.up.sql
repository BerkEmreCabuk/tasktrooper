-- Team-shared memories: agent_id NULL means the memory belongs to the whole team.
ALTER TABLE agent_memories ALTER COLUMN agent_id DROP NOT NULL;

CREATE TABLE IF NOT EXISTS agent_golden_tasks (
    id         UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    agent_id   UUID NOT NULL REFERENCES agents(id) ON DELETE CASCADE,
    name       TEXT NOT NULL,
    prompt     TEXT NOT NULL,
    expected   JSONB NOT NULL DEFAULT '[]',
    enabled    BOOLEAN NOT NULL DEFAULT true,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE(agent_id, name)
);

CREATE TABLE IF NOT EXISTS agent_golden_results (
    id            UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    golden_id     UUID NOT NULL REFERENCES agent_golden_tasks(id) ON DELETE CASCADE,
    agent_id      UUID NOT NULL REFERENCES agents(id) ON DELETE CASCADE,
    reflection_id UUID REFERENCES agent_reflections(id) ON DELETE SET NULL,
    passed        BOOLEAN NOT NULL,
    detail        TEXT NOT NULL DEFAULT '',
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_agent_golden_results_agent
    ON agent_golden_results(agent_id, created_at DESC);
