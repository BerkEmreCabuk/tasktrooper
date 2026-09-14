DROP TABLE IF EXISTS task_documents;
DROP TABLE IF EXISTS task_relations;
DROP TABLE IF EXISTS task_acceptance_criteria;
DROP TABLE IF EXISTS repository_projects;
DROP TABLE IF EXISTS projects;

ALTER TABLE board_tasks DROP CONSTRAINT IF EXISTS board_tasks_task_type_check;
ALTER TABLE board_tasks DROP CONSTRAINT IF EXISTS board_tasks_priority_check;
DROP INDEX IF EXISTS idx_board_tasks_team_number;

ALTER TABLE board_tasks DROP COLUMN IF EXISTS team_id;
ALTER TABLE board_tasks DROP COLUMN IF EXISTS task_type;
ALTER TABLE board_tasks DROP COLUMN IF EXISTS technical_description;
ALTER TABLE board_tasks DROP COLUMN IF EXISTS initiative_project_id;
ALTER TABLE board_tasks DROP COLUMN IF EXISTS team_task_number;
ALTER TABLE board_tasks DROP COLUMN IF EXISTS priority;

DROP INDEX IF EXISTS idx_teams_key_prefix;
ALTER TABLE teams DROP COLUMN IF EXISTS key_prefix;

ALTER TABLE board_events RENAME COLUMN repository_id TO project_id;

ALTER INDEX IF EXISTS idx_task_comments_task RENAME TO idx_project_task_comments_task;
ALTER TABLE task_comments RENAME TO project_task_comments;

ALTER INDEX IF EXISTS idx_board_tasks_column RENAME TO idx_project_tasks_column;
ALTER INDEX IF EXISTS idx_board_tasks_repository RENAME TO idx_project_tasks_project;
ALTER TABLE board_tasks RENAME COLUMN repository_id TO project_id;
ALTER TABLE board_tasks RENAME TO project_tasks;

ALTER INDEX IF EXISTS idx_workspace_indexes_repository RENAME TO idx_workspace_indexes_project;
ALTER TABLE workspace_indexes RENAME COLUMN repository_id TO project_id;

ALTER INDEX IF EXISTS idx_sessions_repository_id RENAME TO idx_sessions_project_id;
ALTER TABLE sessions RENAME COLUMN repository_id TO project_id;

ALTER INDEX IF EXISTS idx_repositories_team_id RENAME TO idx_projects_team_id;
ALTER INDEX IF EXISTS idx_repositories_updated_at RENAME TO idx_projects_updated_at;
ALTER TABLE repositories RENAME TO projects;
