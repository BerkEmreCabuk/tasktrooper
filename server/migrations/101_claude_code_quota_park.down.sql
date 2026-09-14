DROP INDEX IF EXISTS idx_task_agent_runs_quota_resume;

ALTER TABLE task_agent_runs
    DROP COLUMN IF EXISTS cli_session_id,
    DROP COLUMN IF EXISTS quota_resume_at;
