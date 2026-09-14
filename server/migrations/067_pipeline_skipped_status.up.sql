-- A pipeline that ran nothing (no validate/build/test job mapped, no deploy
-- workflow configured) used to be recorded as 'success'. The UI then showed a
-- green "Success" badge next to a provider of "Did not run", and the QA gate
-- looked satisfied by a build that never happened. Such a run is now recorded
-- as 'skipped': it still opens the gate, it just no longer claims a result.
ALTER TABLE task_pipelines DROP CONSTRAINT IF EXISTS task_pipelines_status_check;
ALTER TABLE task_pipelines
    ADD CONSTRAINT task_pipelines_status_check
    CHECK (status IN ('pending','running','success','failed','skipped'));
