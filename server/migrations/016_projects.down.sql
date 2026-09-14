DROP TABLE IF EXISTS project_tasks;
DROP INDEX IF EXISTS idx_workspace_indexes_project;
ALTER TABLE workspace_indexes DROP COLUMN IF EXISTS error;
ALTER TABLE workspace_indexes DROP COLUMN IF EXISTS files_processed;
ALTER TABLE workspace_indexes DROP COLUMN IF EXISTS files_total;
ALTER TABLE workspace_indexes DROP COLUMN IF EXISTS project_id;
ALTER TABLE sessions DROP COLUMN IF EXISTS project_id;
DROP TABLE IF EXISTS projects;
