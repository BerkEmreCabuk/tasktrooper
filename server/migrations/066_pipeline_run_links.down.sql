ALTER TABLE task_pipeline_jobs DROP COLUMN IF EXISTS run_url;
ALTER TABLE task_pipelines DROP COLUMN IF EXISTS provider;
