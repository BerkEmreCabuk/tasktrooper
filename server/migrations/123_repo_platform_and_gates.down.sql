ALTER TABLE repositories
    DROP COLUMN IF EXISTS mobile_platform,
    DROP COLUMN IF EXISTS docs_task_id,
    DROP COLUMN IF EXISTS mutation_enabled,
    DROP COLUMN IF EXISTS mutation_threshold;
