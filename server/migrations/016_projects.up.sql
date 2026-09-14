CREATE TABLE IF NOT EXISTS projects (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    name        TEXT NOT NULL,
    description TEXT NOT NULL DEFAULT '',
    root_path   TEXT NOT NULL UNIQUE,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_projects_updated_at ON projects(updated_at DESC);

ALTER TABLE sessions ADD COLUMN IF NOT EXISTS project_id UUID REFERENCES projects(id) ON DELETE CASCADE;
CREATE INDEX IF NOT EXISTS idx_sessions_project_id ON sessions(project_id);

ALTER TABLE workspace_indexes ADD COLUMN IF NOT EXISTS project_id UUID REFERENCES projects(id) ON DELETE CASCADE;
ALTER TABLE workspace_indexes ADD COLUMN IF NOT EXISTS files_total INT NOT NULL DEFAULT 0;
ALTER TABLE workspace_indexes ADD COLUMN IF NOT EXISTS files_processed INT NOT NULL DEFAULT 0;
ALTER TABLE workspace_indexes ADD COLUMN IF NOT EXISTS error TEXT NOT NULL DEFAULT '';
ALTER TABLE workspace_indexes ALTER COLUMN session_id DROP NOT NULL;

CREATE UNIQUE INDEX IF NOT EXISTS idx_workspace_indexes_project ON workspace_indexes(project_id) WHERE project_id IS NOT NULL;

CREATE TABLE IF NOT EXISTS project_tasks (
    id                UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    project_id        UUID NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    title             TEXT NOT NULL,
    description       TEXT NOT NULL DEFAULT '',
    board_column      TEXT NOT NULL DEFAULT 'backlog',
    position          INT NOT NULL DEFAULT 0,
    created_by        TEXT NOT NULL DEFAULT 'user',
    assignee_agent_id UUID REFERENCES agents(id) ON DELETE SET NULL,
    created_at        TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at        TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_project_tasks_project ON project_tasks(project_id);
CREATE INDEX IF NOT EXISTS idx_project_tasks_column ON project_tasks(project_id, board_column);
