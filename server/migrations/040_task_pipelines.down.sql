DROP TABLE IF EXISTS task_pipeline_jobs;
DROP TABLE IF EXISTS task_pipelines;
ALTER TABLE repositories
    DROP COLUMN IF EXISTS verify_command,
    DROP COLUMN IF EXISTS build_command,
    DROP COLUMN IF EXISTS test_command;
