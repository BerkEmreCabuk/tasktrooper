-- Existing 'skipped' rows must become a value the old constraint accepts, and
-- 'success' is what they were recorded as before this migration.
UPDATE task_pipelines SET status = 'success' WHERE status = 'skipped';
ALTER TABLE task_pipelines DROP CONSTRAINT IF EXISTS task_pipelines_status_check;
ALTER TABLE task_pipelines
    ADD CONSTRAINT task_pipelines_status_check
    CHECK (status IN ('pending','running','success','failed'));
