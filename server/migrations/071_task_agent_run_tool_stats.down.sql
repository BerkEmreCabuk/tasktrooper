DROP INDEX IF EXISTS idx_task_agent_runs_agent_created;

ALTER TABLE task_agent_runs
    DROP COLUMN IF EXISTS tool_calls,
    DROP COLUMN IF EXISTS tool_errors,
    DROP COLUMN IF EXISTS error_pattern;
