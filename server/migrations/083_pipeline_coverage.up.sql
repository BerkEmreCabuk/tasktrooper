-- Test coverage percentage surfaced from the QA pipeline's test job logs.
--
-- The platform now parses the test job's log (success or failure) for a total
-- coverage percentage (see board.ParseCoverage). Both the job row and the
-- pipeline row carry it: the job's own reading, and the pipeline's rollup
-- (the mean across test jobs, when a run has more than one).
--
-- NUMERIC(5,2) allows any value in 0.00..100.00. Nullable: most historical
-- pipelines/jobs never had a parsable coverage line, and a run whose test
-- stage never printed one is "unknown", not "zero".
ALTER TABLE task_pipelines
    ADD COLUMN IF NOT EXISTS coverage_pct NUMERIC(5,2);

ALTER TABLE task_pipeline_jobs
    ADD COLUMN IF NOT EXISTS coverage_pct NUMERIC(5,2);
