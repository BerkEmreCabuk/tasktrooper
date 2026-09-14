-- Remove the team layer: the system becomes a single implicit workspace.
-- Keep the team named 'deneme' (else the oldest team); every other team's
-- data is deleted via FK cascades before the schema is flattened.

-- 1. Keep exactly one team's data.
DELETE FROM teams
WHERE id NOT IN (
    SELECT id FROM teams
    ORDER BY (lower(name) = 'deneme') DESC, created_at ASC
    LIMIT 1
);

-- 2. Global board settings (task key prefix, e.g. DE-42).
CREATE TABLE IF NOT EXISTS board_settings (
    id         SMALLINT PRIMARY KEY DEFAULT 1 CHECK (id = 1),
    key_prefix TEXT NOT NULL DEFAULT 'WS'
);

INSERT INTO board_settings (id, key_prefix)
SELECT 1, COALESCE((SELECT key_prefix FROM teams LIMIT 1), 'WS')
ON CONFLICT (id) DO NOTHING;

-- 3. Global board columns.
CREATE TABLE IF NOT EXISTS board_columns (
    id         UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    slug       TEXT NOT NULL UNIQUE,
    label      TEXT NOT NULL,
    position   INT NOT NULL DEFAULT 0,
    is_backlog BOOLEAN NOT NULL DEFAULT false
);

INSERT INTO board_columns (slug, label, position, is_backlog)
SELECT slug, label, position, is_backlog FROM team_columns
ON CONFLICT (slug) DO NOTHING;

INSERT INTO board_columns (slug, label, position, is_backlog)
SELECT v.slug, v.label, v.position, v.is_backlog
FROM (VALUES
    ('backlog', 'Backlog', 0, true),
    ('todo', 'Todo', 1, false),
    ('in_progress', 'In Progress', 2, false),
    ('ready_for_qa', 'Ready for QA', 3, false),
    ('in_qa', 'In QA', 4, false),
    ('need_revision', 'Need Revision', 5, false),
    ('pm_uat', 'PM UAT', 6, false),
    ('human_uat', 'Human UAT', 7, false),
    ('done', 'Done', 8, false),
    ('released', 'Released', 9, false)
) AS v(slug, label, position, is_backlog)
WHERE NOT EXISTS (SELECT 1 FROM board_columns);

-- 4. Global board membership.
CREATE TABLE IF NOT EXISTS board_members (
    agent_id UUID PRIMARY KEY REFERENCES agents(id) ON DELETE CASCADE
);

INSERT INTO board_members (agent_id)
SELECT DISTINCT agent_id FROM team_members
ON CONFLICT DO NOTHING;

-- 5. Global column subscriptions.
CREATE TABLE IF NOT EXISTS agent_column_subscriptions (
    agent_id         UUID NOT NULL REFERENCES agents(id) ON DELETE CASCADE,
    column_slug      TEXT NOT NULL,
    task_type_filter TEXT[] DEFAULT NULL,
    PRIMARY KEY (agent_id, column_slug)
);

INSERT INTO agent_column_subscriptions (agent_id, column_slug, task_type_filter)
SELECT agent_id, column_slug, task_type_filter FROM team_agent_column_subscriptions
ON CONFLICT DO NOTHING;

-- 6. Performance scores: one row per agent. Prefer the team-scoped row
-- (migration 034) over the legacy global row when both exist.
DELETE FROM agent_performance_scores s
WHERE s.team_id IS NULL
  AND EXISTS (
      SELECT 1 FROM agent_performance_scores t
      WHERE t.agent_id = s.agent_id AND t.team_id IS NOT NULL
  );

DROP INDEX IF EXISTS uq_agent_scores_agent_team;
DROP INDEX IF EXISTS uq_agent_scores_agent_legacy;
ALTER TABLE agent_performance_scores DROP COLUMN IF EXISTS team_id;
CREATE UNIQUE INDEX IF NOT EXISTS uq_agent_scores_agent ON agent_performance_scores(agent_id);

DROP INDEX IF EXISTS idx_agent_score_events_agent_team;
ALTER TABLE agent_score_events DROP COLUMN IF EXISTS team_id;

-- 7. Memories, reflections, evolution events, KPI results: agent-global.
DROP INDEX IF EXISTS idx_agent_memories_agent_team;
ALTER TABLE agent_memories DROP COLUMN IF EXISTS team_id;
CREATE INDEX IF NOT EXISTS idx_agent_memories_agent ON agent_memories(agent_id, created_at DESC);

DROP INDEX IF EXISTS idx_agent_reflections_agent_team;
ALTER TABLE agent_reflections DROP COLUMN IF EXISTS team_id;
CREATE INDEX IF NOT EXISTS idx_agent_reflections_agent ON agent_reflections(agent_id, created_at DESC);

DROP INDEX IF EXISTS idx_agent_evolution_events_agent_team;
ALTER TABLE agent_evolution_events DROP COLUMN IF EXISTS team_id;
CREATE INDEX IF NOT EXISTS idx_agent_evolution_events_agent ON agent_evolution_events(agent_id, created_at DESC);

DROP INDEX IF EXISTS idx_agent_kpi_results_agent_team;
ALTER TABLE agent_kpi_results DROP COLUMN IF EXISTS team_id;
ALTER TABLE agent_kpi_results ADD CONSTRAINT uq_agent_kpi_results_kpi_period UNIQUE (kpi_id, period_start);

-- 8. Board tasks: global task numbering.
DROP INDEX IF EXISTS idx_board_tasks_team_number;
ALTER TABLE board_tasks RENAME COLUMN team_task_number TO task_number;
ALTER TABLE board_tasks DROP COLUMN IF EXISTS team_id;
CREATE UNIQUE INDEX IF NOT EXISTS idx_board_tasks_task_number ON board_tasks(task_number);

-- 9. Drop team scope from the remaining tables.
DROP INDEX IF EXISTS idx_board_events_team;
ALTER TABLE board_events DROP COLUMN IF EXISTS team_id;
CREATE INDEX IF NOT EXISTS idx_board_events_created ON board_events(created_at DESC);

ALTER TABLE sessions DROP COLUMN IF EXISTS team_id;

DROP INDEX IF EXISTS idx_repositories_team_id;
ALTER TABLE repositories DROP COLUMN IF EXISTS team_id;

DROP INDEX IF EXISTS idx_projects_team_id;
ALTER TABLE projects DROP COLUMN IF EXISTS team_id;

-- 10. Drop the team tables.
DROP TABLE IF EXISTS team_agent_column_subscriptions;
DROP TABLE IF EXISTS team_members;
DROP TABLE IF EXISTS team_columns;
DROP TABLE IF EXISTS teams;
