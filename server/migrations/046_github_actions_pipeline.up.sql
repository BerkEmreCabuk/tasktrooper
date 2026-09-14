-- GitHub Actions-backed QA pipeline & release triggering.
-- Repo type + monorepo sub-repos + auto-release toggle, plus a per-(sub-)repo
-- category → GitHub Actions job/workflow mapping.

ALTER TABLE repositories
    ADD COLUMN IF NOT EXISTS kind                 TEXT NOT NULL DEFAULT 'backend',
    ADD COLUMN IF NOT EXISTS sub_repo_kinds       TEXT[] NOT NULL DEFAULT '{}',
    ADD COLUMN IF NOT EXISTS auto_release_on_done BOOLEAN NOT NULL DEFAULT true;

-- Category → GitHub Actions target mapping.
--   category:    validate | build | test | stage_deploy | prod_deploy
--   target_kind: 'job'      (validate/build/test — a run job to read status from)
--                'workflow' (stage_deploy/prod_deploy — a workflow file to dispatch)
--   sub_repo_kind: '' for single-kind repos, else backend/frontend/mobile/worker
CREATE TABLE IF NOT EXISTS repository_pipeline_jobs (
    id            UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    repository_id UUID NOT NULL REFERENCES repositories(id) ON DELETE CASCADE,
    sub_repo_kind TEXT NOT NULL DEFAULT '',
    category      TEXT NOT NULL CHECK (category IN ('validate','build','test','stage_deploy','prod_deploy')),
    target_kind   TEXT NOT NULL DEFAULT 'job' CHECK (target_kind IN ('job','workflow')),
    target_ref    TEXT NOT NULL DEFAULT '',
    auto_detected BOOLEAN NOT NULL DEFAULT false,
    updated_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (repository_id, sub_repo_kind, category)
);
CREATE INDEX IF NOT EXISTS idx_repository_pipeline_jobs_repo ON repository_pipeline_jobs(repository_id);

-- Allow deploy pipelines to be recorded as task_pipelines alongside the QA gate.
ALTER TABLE task_pipelines DROP CONSTRAINT IF EXISTS task_pipelines_trigger_check;
ALTER TABLE task_pipelines
    ADD CONSTRAINT task_pipelines_trigger_check
    CHECK (trigger IN ('ready_for_qa','manual','retry','stage_deploy','prod_deploy'));
