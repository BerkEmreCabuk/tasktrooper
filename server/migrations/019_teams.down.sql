DROP TABLE IF EXISTS task_agent_runs;
DROP TABLE IF EXISTS board_events;
DROP TABLE IF EXISTS project_task_comments;
ALTER TABLE projects DROP COLUMN IF EXISTS team_id;
DROP TABLE IF EXISTS team_agent_column_subscriptions;
DROP TABLE IF EXISTS team_members;
DROP TABLE IF EXISTS team_columns;
DROP TABLE IF EXISTS teams;
