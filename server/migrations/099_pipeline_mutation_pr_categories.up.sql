-- Add two pipeline categories that CI already runs but the mapping could not
-- name:
--   mutation_test — a job (read like validate/build/test). Deliberately
--                   non-blocking: the workflow side marks it continue-on-error,
--                   so a surviving mutant reports without failing the QA gate.
--   pr_open       — a workflow (the repo's auto-pr.yml). Informational only: it
--                   is never dispatched and never gated, because agent task
--                   branches (tt-123) do not match its feature/** trigger — the
--                   board opens their PR itself via EnsureDraftPR.

ALTER TABLE repository_pipeline_jobs DROP CONSTRAINT IF EXISTS repository_pipeline_jobs_category_check;
ALTER TABLE repository_pipeline_jobs
    ADD CONSTRAINT repository_pipeline_jobs_category_check
    CHECK (category IN ('validate','build','test','mutation_test','pr_open','stage_deploy','preprod_deploy','prod_deploy'));
