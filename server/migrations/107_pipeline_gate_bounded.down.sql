-- Written for symmetry with the rest of migrations/; nothing executes .down.sql
-- (migrations/embed.go embeds only *.up.sql and there is no rollback path).

DROP INDEX IF EXISTS idx_task_pipelines_head_sha;
DROP INDEX IF EXISTS idx_task_pipelines_unfinished;

ALTER TABLE task_pipelines
    DROP COLUMN IF EXISTS gate_reason,
    DROP COLUMN IF EXISTS head_sha;

ALTER TABLE repositories
    DROP COLUMN IF EXISTS require_pipeline_for_review;
