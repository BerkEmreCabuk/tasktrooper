-- Written for symmetry with the rest of migrations/; nothing executes .down.sql
-- (migrations/embed.go embeds only *.up.sql and there is no rollback path).
DROP INDEX IF EXISTS idx_board_tasks_initiative_project;
DROP INDEX IF EXISTS idx_task_agent_runs_session_run;
DROP INDEX IF EXISTS idx_task_criterion_checks_agent;
DROP INDEX IF EXISTS idx_agent_score_events_task;
DROP INDEX IF EXISTS idx_task_pipelines_repository;
DROP INDEX IF EXISTS idx_task_column_spans_repository;
DROP INDEX IF EXISTS idx_task_pipelines_active;
DROP INDEX IF EXISTS idx_task_agent_runs_active;
DROP INDEX IF EXISTS idx_sessions_agent_updated;
