ALTER TABLE projects RENAME TO repositories;

ALTER INDEX IF EXISTS idx_projects_updated_at RENAME TO idx_repositories_updated_at;
ALTER INDEX IF EXISTS idx_projects_team_id RENAME TO idx_repositories_team_id;

ALTER TABLE sessions RENAME COLUMN project_id TO repository_id;
ALTER INDEX IF EXISTS idx_sessions_project_id RENAME TO idx_sessions_repository_id;

ALTER TABLE workspace_indexes RENAME COLUMN project_id TO repository_id;
ALTER INDEX IF EXISTS idx_workspace_indexes_project RENAME TO idx_workspace_indexes_repository;

ALTER TABLE project_tasks RENAME TO board_tasks;
ALTER TABLE board_tasks RENAME COLUMN project_id TO repository_id;
ALTER INDEX IF EXISTS idx_project_tasks_project RENAME TO idx_board_tasks_repository;
ALTER INDEX IF EXISTS idx_project_tasks_column RENAME TO idx_board_tasks_column;

ALTER TABLE project_task_comments RENAME TO task_comments;
ALTER INDEX IF EXISTS idx_project_task_comments_task RENAME TO idx_task_comments_task;

ALTER TABLE board_events RENAME COLUMN project_id TO repository_id;

ALTER TABLE teams ADD COLUMN IF NOT EXISTS key_prefix TEXT;

UPDATE teams SET key_prefix = upper(substring(regexp_replace(name, '[^a-zA-Z]', '', 'g') from 1 for 2))
WHERE key_prefix IS NULL OR key_prefix = '';

UPDATE teams SET key_prefix = 'T' || substring(id::text from 1 for 1)
WHERE key_prefix IS NULL OR key_prefix = '' OR length(key_prefix) < 2;

UPDATE teams t SET key_prefix = t.key_prefix || '2'
WHERE EXISTS (
    SELECT 1 FROM teams t2
    WHERE t2.key_prefix = t.key_prefix AND t2.id <> t.id AND t2.id < t.id
);

ALTER TABLE teams ALTER COLUMN key_prefix SET NOT NULL;
CREATE UNIQUE INDEX IF NOT EXISTS idx_teams_key_prefix ON teams(key_prefix);

CREATE TABLE IF NOT EXISTS projects (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    team_id     UUID NOT NULL REFERENCES teams(id) ON DELETE CASCADE,
    name        TEXT NOT NULL,
    description TEXT NOT NULL DEFAULT '',
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_projects_team_id ON projects(team_id);

CREATE TABLE IF NOT EXISTS repository_projects (
    repository_id UUID NOT NULL REFERENCES repositories(id) ON DELETE CASCADE,
    project_id    UUID NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    PRIMARY KEY (repository_id, project_id)
);

ALTER TABLE board_tasks ADD COLUMN IF NOT EXISTS team_id UUID REFERENCES teams(id) ON DELETE CASCADE;
ALTER TABLE board_tasks ADD COLUMN IF NOT EXISTS task_type TEXT NOT NULL DEFAULT 'task';
ALTER TABLE board_tasks ADD COLUMN IF NOT EXISTS technical_description TEXT NOT NULL DEFAULT '';
ALTER TABLE board_tasks ADD COLUMN IF NOT EXISTS initiative_project_id UUID REFERENCES projects(id) ON DELETE SET NULL;
ALTER TABLE board_tasks ADD COLUMN IF NOT EXISTS team_task_number INT;
ALTER TABLE board_tasks ADD COLUMN IF NOT EXISTS priority TEXT NOT NULL DEFAULT 'medium';

UPDATE board_tasks bt
SET team_id = r.team_id
FROM repositories r
WHERE bt.repository_id = r.id AND bt.team_id IS NULL;

WITH numbered AS (
    SELECT id, row_number() OVER (PARTITION BY team_id ORDER BY created_at ASC) AS num
    FROM board_tasks
    WHERE team_task_number IS NULL
)
UPDATE board_tasks bt
SET team_task_number = numbered.num
FROM numbered
WHERE bt.id = numbered.id;

ALTER TABLE board_tasks ALTER COLUMN team_id SET NOT NULL;
ALTER TABLE board_tasks ALTER COLUMN team_task_number SET NOT NULL;

CREATE UNIQUE INDEX IF NOT EXISTS idx_board_tasks_team_number ON board_tasks(team_id, team_task_number);

ALTER TABLE board_tasks ADD CONSTRAINT board_tasks_task_type_check
    CHECK (task_type IN ('task', 'analiz', 'bug'));

ALTER TABLE board_tasks ADD CONSTRAINT board_tasks_priority_check
    CHECK (priority IN ('low', 'medium', 'high', 'critical'));

CREATE TABLE IF NOT EXISTS task_acceptance_criteria (
    id         UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    task_id    UUID NOT NULL REFERENCES board_tasks(id) ON DELETE CASCADE,
    text       TEXT NOT NULL,
    position   INT NOT NULL DEFAULT 0,
    completed  BOOLEAN NOT NULL DEFAULT false,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_task_acceptance_criteria_task ON task_acceptance_criteria(task_id, position);

CREATE TABLE IF NOT EXISTS task_relations (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    source_task_id  UUID NOT NULL REFERENCES board_tasks(id) ON DELETE CASCADE,
    target_task_id  UUID NOT NULL REFERENCES board_tasks(id) ON DELETE CASCADE,
    relation_type   TEXT NOT NULL DEFAULT 'blocks',
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (source_task_id, target_task_id, relation_type),
    CHECK (source_task_id <> target_task_id),
    CHECK (relation_type IN ('blocks'))
);

CREATE INDEX IF NOT EXISTS idx_task_relations_source ON task_relations(source_task_id);
CREATE INDEX IF NOT EXISTS idx_task_relations_target ON task_relations(target_task_id);

CREATE TABLE IF NOT EXISTS task_documents (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    task_id         UUID NOT NULL REFERENCES board_tasks(id) ON DELETE CASCADE,
    title           TEXT NOT NULL,
    content         TEXT NOT NULL DEFAULT '',
    position        INT NOT NULL DEFAULT 0,
    created_by_type TEXT NOT NULL DEFAULT 'user' CHECK (created_by_type IN ('user', 'agent')),
    created_by_id   TEXT NOT NULL DEFAULT '',
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_task_documents_task ON task_documents(task_id, position);
