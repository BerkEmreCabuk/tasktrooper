DROP TABLE IF EXISTS repository_pipeline_jobs;

ALTER TABLE repositories
    DROP COLUMN IF EXISTS kind,
    DROP COLUMN IF EXISTS sub_repo_kinds,
    DROP COLUMN IF EXISTS auto_release_on_done;

ALTER TABLE task_pipelines DROP CONSTRAINT IF EXISTS task_pipelines_trigger_check;
ALTER TABLE task_pipelines
    ADD CONSTRAINT task_pipelines_trigger_check
    CHECK (trigger IN ('ready_for_qa','manual','retry'));
