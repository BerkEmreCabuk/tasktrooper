-- Revert preprod_deploy. Rows referencing it must be cleared before the
-- narrower CHECK constraints can be re-added.

DELETE FROM repository_pipeline_jobs WHERE category = 'preprod_deploy';
DELETE FROM task_pipelines WHERE trigger = 'preprod_deploy';

ALTER TABLE repository_pipeline_jobs DROP CONSTRAINT IF EXISTS repository_pipeline_jobs_category_check;
ALTER TABLE repository_pipeline_jobs
    ADD CONSTRAINT repository_pipeline_jobs_category_check
    CHECK (category IN ('validate','build','test','stage_deploy','prod_deploy'));

ALTER TABLE task_pipelines DROP CONSTRAINT IF EXISTS task_pipelines_trigger_check;
ALTER TABLE task_pipelines
    ADD CONSTRAINT task_pipelines_trigger_check
    CHECK (trigger IN ('ready_for_qa','manual','retry','stage_deploy','prod_deploy'));
