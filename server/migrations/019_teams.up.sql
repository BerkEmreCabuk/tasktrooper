CREATE TABLE IF NOT EXISTS teams (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    name        TEXT NOT NULL,
    description TEXT NOT NULL DEFAULT '',
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);

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

CREATE TABLE IF NOT EXISTS team_members (
    team_id  UUID NOT NULL REFERENCES teams(id) ON DELETE CASCADE,
    agent_id UUID NOT NULL REFERENCES agents(id) ON DELETE CASCADE,
    PRIMARY KEY (team_id, agent_id)
);

CREATE TABLE IF NOT EXISTS team_agent_column_subscriptions (
    team_id     UUID NOT NULL REFERENCES teams(id) ON DELETE CASCADE,
    agent_id    UUID NOT NULL REFERENCES agents(id) ON DELETE CASCADE,
    column_slug TEXT NOT NULL,
    PRIMARY KEY (team_id, agent_id, column_slug)
);

ALTER TABLE projects ADD COLUMN IF NOT EXISTS team_id UUID REFERENCES teams(id) ON DELETE CASCADE;

INSERT INTO teams (id, name, description)
SELECT '00000000-0000-0000-0000-000000000001'::uuid, 'Default Team', 'Migrated projects'
WHERE NOT EXISTS (SELECT 1 FROM teams WHERE id = '00000000-0000-0000-0000-000000000001'::uuid);

UPDATE projects SET team_id = '00000000-0000-0000-0000-000000000001'::uuid WHERE team_id IS NULL;

INSERT INTO team_columns (team_id, slug, label, position, is_backlog)
SELECT '00000000-0000-0000-0000-000000000001'::uuid, v.slug, v.label, v.position, v.is_backlog
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
ON CONFLICT DO NOTHING;

ALTER TABLE projects ALTER COLUMN team_id SET NOT NULL;

CREATE INDEX IF NOT EXISTS idx_projects_team_id ON projects(team_id);

CREATE TABLE IF NOT EXISTS project_task_comments (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    task_id     UUID NOT NULL REFERENCES project_tasks(id) ON DELETE CASCADE,
    author_type TEXT NOT NULL CHECK (author_type IN ('user', 'agent')),
    author_id   TEXT NOT NULL DEFAULT '',
    content     TEXT NOT NULL,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_project_task_comments_task ON project_task_comments(task_id, created_at);

CREATE TABLE IF NOT EXISTS board_events (
    id         UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    team_id    UUID NOT NULL REFERENCES teams(id) ON DELETE CASCADE,
    project_id UUID NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    task_id    UUID NOT NULL REFERENCES project_tasks(id) ON DELETE CASCADE,
    event_type TEXT NOT NULL,
    payload    JSONB NOT NULL DEFAULT '{}',
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_board_events_team ON board_events(team_id, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_board_events_task ON board_events(task_id, created_at DESC);

CREATE TABLE IF NOT EXISTS task_agent_runs (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    task_id         UUID NOT NULL REFERENCES project_tasks(id) ON DELETE CASCADE,
    agent_id        UUID NOT NULL REFERENCES agents(id) ON DELETE CASCADE,
    board_event_id  UUID NOT NULL REFERENCES board_events(id) ON DELETE CASCADE,
    session_run_id  UUID REFERENCES session_runs(id) ON DELETE SET NULL,
    status          TEXT NOT NULL DEFAULT 'pending',
    summary         TEXT NOT NULL DEFAULT '',
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_task_agent_runs_task ON task_agent_runs(task_id, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_task_agent_runs_event ON task_agent_runs(board_event_id, agent_id);
