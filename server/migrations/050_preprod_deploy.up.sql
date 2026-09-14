-- Add preprod_deploy: a third, optional deploy environment between stage and
-- prod. Extends the category (repository_pipeline_jobs) and trigger
-- (task_pipelines) CHECK constraints.

ALTER TABLE repository_pipeline_jobs DROP CONSTRAINT IF EXISTS repository_pipeline_jobs_category_check;
ALTER TABLE repository_pipeline_jobs
    ADD CONSTRAINT repository_pipeline_jobs_category_check
    CHECK (category IN ('validate','build','test','stage_deploy','preprod_deploy','prod_deploy'));

ALTER TABLE task_pipelines DROP CONSTRAINT IF EXISTS task_pipelines_trigger_check;
ALTER TABLE task_pipelines
    ADD CONSTRAINT task_pipelines_trigger_check
    CHECK (trigger IN ('ready_for_qa','manual','retry','stage_deploy','preprod_deploy','prod_deploy'));
