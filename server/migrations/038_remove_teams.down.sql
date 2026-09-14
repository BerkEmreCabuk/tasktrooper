-- Recreate the team layer with a single default team and attach all data to it.
-- Data deleted by the up migration is not restored.

CREATE TABLE IF NOT EXISTS teams (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    name        TEXT NOT NULL,
    description TEXT NOT NULL DEFAULT '',
    key_prefix  TEXT NOT NULL,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE UNIQUE INDEX IF NOT EXISTS idx_teams_key_prefix ON teams(key_prefix);

INSERT INTO teams (id, name, description, key_prefix)
SELECT '00000000-0000-0000-0000-000000000001'::uuid, 'Default Team', 'Restored from single workspace',
       COALESCE((SELECT key_prefix FROM board_settings WHERE id = 1), 'WS')
WHERE NOT EXISTS (SELECT 1 FROM teams WHERE id = '00000000-0000-0000-0000-000000000001'::uuid);

CREATE TABLE IF NOT EXISTS team_columns (
    id         UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    team_id    UUID NOT NULL REFERENCES teams(id) ON DELETE CASCADE,
    slug       TEXT NOT NULL,
    label      TEXT NOT NULL,
    position   INT NOT NULL DEFAULT 0,
    is_backlog BOOLEAN NOT NULL DEFAULT false,
    UNIQUE (team_id, slug)
);
CREATE INDEX IF NOT EXISTS idx_team_columns_team ON team_columns(team_id, position);

INSERT INTO team_columns (team_id, slug, label, position, is_backlog)
SELECT '00000000-0000-0000-0000-000000000001'::uuid, slug, label, position, is_backlog
FROM board_columns
ON CONFLICT DO NOTHING;

CREATE TABLE IF NOT EXISTS team_members (
    team_id  UUID NOT NULL REFERENCES teams(id) ON DELETE CASCADE,
    agent_id UUID NOT NULL REFERENCES agents(id) ON DELETE CASCADE,
    PRIMARY KEY (team_id, agent_id)
);

INSERT INTO team_members (team_id, agent_id)
SELECT '00000000-0000-0000-0000-000000000001'::uuid, agent_id
FROM board_members
ON CONFLICT DO NOTHING;

CREATE TABLE IF NOT EXISTS team_agent_column_subscriptions (
    team_id          UUID NOT NULL REFERENCES teams(id) ON DELETE CASCADE,
    agent_id         UUID NOT NULL REFERENCES agents(id) ON DELETE CASCADE,
    column_slug      TEXT NOT NULL,
    task_type_filter TEXT[] DEFAULT NULL,
    PRIMARY KEY (team_id, agent_id, column_slug)
);

INSERT INTO team_agent_column_subscriptions (team_id, agent_id, column_slug, task_type_filter)
SELECT '00000000-0000-0000-0000-000000000001'::uuid, agent_id, column_slug, task_type_filter
FROM agent_column_subscriptions
ON CONFLICT DO NOTHING;

-- Restore team_id columns.
ALTER TABLE agent_performance_scores ADD COLUMN IF NOT EXISTS team_id UUID REFERENCES teams(id) ON DELETE CASCADE;
UPDATE agent_performance_scores SET team_id = '00000000-0000-0000-0000-000000000001'::uuid WHERE team_id IS NULL;
DROP INDEX IF EXISTS uq_agent_scores_agent;
CREATE UNIQUE INDEX IF NOT EXISTS uq_agent_scores_agent_team
    ON agent_performance_scores(agent_id, team_id) WHERE team_id IS NOT NULL;
CREATE UNIQUE INDEX IF NOT EXISTS uq_agent_scores_agent_legacy
    ON agent_performance_scores(agent_id) WHERE team_id IS NULL;

ALTER TABLE agent_score_events ADD COLUMN IF NOT EXISTS team_id UUID REFERENCES teams(id) ON DELETE CASCADE;
UPDATE agent_score_events SET team_id = '00000000-0000-0000-0000-000000000001'::uuid WHERE team_id IS NULL;
CREATE INDEX IF NOT EXISTS idx_agent_score_events_agent_team
    ON agent_score_events(agent_id, team_id, created_at DESC);

ALTER TABLE agent_memories ADD COLUMN IF NOT EXISTS team_id UUID REFERENCES teams(id) ON DELETE CASCADE;
UPDATE agent_memories SET team_id = '00000000-0000-0000-0000-000000000001'::uuid WHERE team_id IS NULL;
ALTER TABLE agent_memories ALTER COLUMN team_id SET NOT NULL;
DROP INDEX IF EXISTS idx_agent_memories_agent;
CREATE INDEX IF NOT EXISTS idx_agent_memories_agent_team ON agent_memories(agent_id, team_id, created_at DESC);

ALTER TABLE agent_reflections ADD COLUMN IF NOT EXISTS team_id UUID REFERENCES teams(id) ON DELETE CASCADE;
UPDATE agent_reflections SET team_id = '00000000-0000-0000-0000-000000000001'::uuid WHERE team_id IS NULL;
ALTER TABLE agent_reflections ALTER COLUMN team_id SET NOT NULL;
DROP INDEX IF EXISTS idx_agent_reflections_agent;
CREATE INDEX IF NOT EXISTS idx_agent_reflections_agent_team ON agent_reflections(agent_id, team_id, created_at DESC);

ALTER TABLE agent_evolution_events ADD COLUMN IF NOT EXISTS team_id UUID REFERENCES teams(id) ON DELETE CASCADE;
UPDATE agent_evolution_events SET team_id = '00000000-0000-0000-0000-000000000001'::uuid WHERE team_id IS NULL;
ALTER TABLE agent_evolution_events ALTER COLUMN team_id SET NOT NULL;
DROP INDEX IF EXISTS idx_agent_evolution_events_agent;
CREATE INDEX IF NOT EXISTS idx_agent_evolution_events_agent_team ON agent_evolution_events(agent_id, team_id, created_at DESC);

ALTER TABLE agent_kpi_results DROP CONSTRAINT IF EXISTS uq_agent_kpi_results_kpi_period;
ALTER TABLE agent_kpi_results ADD COLUMN IF NOT EXISTS team_id UUID REFERENCES teams(id) ON DELETE CASCADE;
UPDATE agent_kpi_results SET team_id = '00000000-0000-0000-0000-000000000001'::uuid WHERE team_id IS NULL;
ALTER TABLE agent_kpi_results ALTER COLUMN team_id SET NOT NULL;
CREATE INDEX IF NOT EXISTS idx_agent_kpi_results_agent_team ON agent_kpi_results(agent_id, team_id, period_start DESC);

DROP INDEX IF EXISTS idx_board_tasks_task_number;
ALTER TABLE board_tasks RENAME COLUMN task_number TO team_task_number;
ALTER TABLE board_tasks ADD COLUMN IF NOT EXISTS team_id UUID REFERENCES teams(id) ON DELETE CASCADE;
UPDATE board_tasks SET team_id = '00000000-0000-0000-0000-000000000001'::uuid WHERE team_id IS NULL;
ALTER TABLE board_tasks ALTER COLUMN team_id SET NOT NULL;
CREATE UNIQUE INDEX IF NOT EXISTS idx_board_tasks_team_number ON board_tasks(team_id, team_task_number);

DROP INDEX IF EXISTS idx_board_events_created;
ALTER TABLE board_events ADD COLUMN IF NOT EXISTS team_id UUID REFERENCES teams(id) ON DELETE CASCADE;
UPDATE board_events SET team_id = '00000000-0000-0000-0000-000000000001'::uuid WHERE team_id IS NULL;
ALTER TABLE board_events ALTER COLUMN team_id SET NOT NULL;
CREATE INDEX IF NOT EXISTS idx_board_events_team ON board_events(team_id, created_at DESC);

ALTER TABLE sessions ADD COLUMN IF NOT EXISTS team_id UUID REFERENCES teams(id) ON DELETE CASCADE;

ALTER TABLE repositories ADD COLUMN IF NOT EXISTS team_id UUID REFERENCES teams(id) ON DELETE CASCADE;
UPDATE repositories SET team_id = '00000000-0000-0000-0000-000000000001'::uuid WHERE team_id IS NULL;
ALTER TABLE repositories ALTER COLUMN team_id SET NOT NULL;
CREATE INDEX IF NOT EXISTS idx_repositories_team_id ON repositories(team_id);

ALTER TABLE projects ADD COLUMN IF NOT EXISTS team_id UUID REFERENCES teams(id) ON DELETE CASCADE;
UPDATE projects SET team_id = '00000000-0000-0000-0000-000000000001'::uuid WHERE team_id IS NULL;
ALTER TABLE projects ALTER COLUMN team_id SET NOT NULL;
CREATE INDEX IF NOT EXISTS idx_projects_team_id ON projects(team_id);

DROP TABLE IF EXISTS agent_column_subscriptions;
DROP TABLE IF EXISTS board_members;
DROP TABLE IF EXISTS board_columns;
DROP TABLE IF EXISTS board_settings;
