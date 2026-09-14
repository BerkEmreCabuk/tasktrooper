CREATE TABLE IF NOT EXISTS orchestrator_rules (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    name        TEXT NOT NULL UNIQUE,
    content     TEXT NOT NULL,
    priority    INT NOT NULL DEFAULT 0,
    enabled     BOOLEAN NOT NULL DEFAULT true,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_orchestrator_rules_priority ON orchestrator_rules(priority DESC);

CREATE TABLE IF NOT EXISTS skills (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    name        TEXT NOT NULL UNIQUE,
    description TEXT NOT NULL DEFAULT '',
    category    TEXT NOT NULL DEFAULT '',
    tags        TEXT[] NOT NULL DEFAULT '{}',
    content     TEXT NOT NULL,
    embedding   JSONB,
    enabled     BOOLEAN NOT NULL DEFAULT true,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_skills_category ON skills(category);
CREATE INDEX IF NOT EXISTS idx_skills_enabled ON skills(enabled);

CREATE TABLE IF NOT EXISTS agents (
    id             UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    name           TEXT NOT NULL UNIQUE,
    description    TEXT NOT NULL DEFAULT '',
    subagent_type  TEXT NOT NULL DEFAULT 'generalPurpose',
    system_prompt  TEXT NOT NULL DEFAULT '',
    model          TEXT NOT NULL DEFAULT '',
    tool_policy    JSONB NOT NULL DEFAULT '{}',
    enabled        BOOLEAN NOT NULL DEFAULT true,
    created_at     TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_agents_enabled ON agents(enabled);

CREATE TABLE IF NOT EXISTS agent_skills (
    agent_id  UUID NOT NULL REFERENCES agents(id) ON DELETE CASCADE,
    skill_id  UUID NOT NULL REFERENCES skills(id) ON DELETE CASCADE,
    PRIMARY KEY (agent_id, skill_id)
);

CREATE TABLE IF NOT EXISTS orchestration_plans (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    run_id      UUID NOT NULL REFERENCES session_runs(id) ON DELETE CASCADE,
    status      TEXT NOT NULL DEFAULT 'pending',
    plan_json   JSONB NOT NULL DEFAULT '{}',
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_orchestration_plans_run_id ON orchestration_plans(run_id);
CREATE INDEX IF NOT EXISTS idx_orchestration_plans_status ON orchestration_plans(status);

CREATE TABLE IF NOT EXISTS plan_tasks (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    plan_id     UUID NOT NULL REFERENCES orchestration_plans(id) ON DELETE CASCADE,
    task_key    TEXT NOT NULL,
    title       TEXT NOT NULL,
    description TEXT NOT NULL DEFAULT '',
    agent_id    UUID REFERENCES agents(id) ON DELETE SET NULL,
    skill_ids   UUID[] NOT NULL DEFAULT '{}',
    depends_on  TEXT[] NOT NULL DEFAULT '{}',
    status      TEXT NOT NULL DEFAULT 'pending',
    result      TEXT,
    error       TEXT,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (plan_id, task_key)
);

CREATE INDEX IF NOT EXISTS idx_plan_tasks_plan_id ON plan_tasks(plan_id);
CREATE INDEX IF NOT EXISTS idx_plan_tasks_status ON plan_tasks(status);
