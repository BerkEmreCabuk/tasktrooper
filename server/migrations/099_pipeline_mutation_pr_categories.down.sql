-- Rows in the dropped categories must go before the narrower CHECK can hold.
DELETE FROM repository_pipeline_jobs WHERE category IN ('mutation_test','pr_open');

ALTER TABLE repository_pipeline_jobs DROP CONSTRAINT IF EXISTS repository_pipeline_jobs_category_check;
ALTER TABLE repository_pipeline_jobs
    ADD CONSTRAINT repository_pipeline_jobs_category_check
    CHECK (category IN ('validate','build','test','stage_deploy','preprod_deploy','prod_deploy'));
