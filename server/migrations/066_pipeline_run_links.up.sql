-- Where a pipeline actually ran, and how to reach it. Until now a run showed
-- only pass/fail: the UI could not say whether GitHub Actions executed it or
-- whether nothing ran at all, and there was no way to open the run on GitHub.
-- provider: '' (unknown/legacy) | 'github_actions' | 'none' (nothing to run)
ALTER TABLE task_pipelines
    ADD COLUMN IF NOT EXISTS provider TEXT NOT NULL DEFAULT '';

-- run_url is the Actions job/run page for this job; empty when it never ran
-- on a provider (skipped, or a legacy row).
ALTER TABLE task_pipeline_jobs
    ADD COLUMN IF NOT EXISTS run_url TEXT NOT NULL DEFAULT '';
