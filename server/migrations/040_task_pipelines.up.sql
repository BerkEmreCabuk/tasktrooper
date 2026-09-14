-- Task QA pipeline: pipeline and job tracking for automated test/build/verify runs.
ALTER TABLE repositories
    ADD COLUMN IF NOT EXISTS verify_command TEXT NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS build_command  TEXT NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS test_command   TEXT NOT NULL DEFAULT '';

CREATE TABLE IF NOT EXISTS task_pipelines (
    id            UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    task_id       UUID NOT NULL REFERENCES board_tasks(id) ON DELETE CASCADE,
    repository_id UUID NOT NULL REFERENCES repositories(id) ON DELETE CASCADE,
    trigger       TEXT NOT NULL CHECK (trigger IN ('ready_for_qa','manual','retry')),
    status        TEXT NOT NULL DEFAULT 'pending' CHECK (status IN ('pending','running','success','failed')),
    note          TEXT NOT NULL DEFAULT '',
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    started_at    TIMESTAMPTZ,
    finished_at   TIMESTAMPTZ
);
CREATE INDEX IF NOT EXISTS idx_task_pipelines_task ON task_pipelines(task_id, created_at DESC);

CREATE TABLE IF NOT EXISTS task_pipeline_jobs (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    pipeline_id UUID NOT NULL REFERENCES task_pipelines(id) ON DELETE CASCADE,
    name        TEXT NOT NULL,
    command     TEXT NOT NULL,
    status      TEXT NOT NULL DEFAULT 'pending' CHECK (status IN ('pending','running','success','failed','skipped')),
    exit_code   INT,
    output      TEXT NOT NULL DEFAULT '',
    duration_ms BIGINT NOT NULL DEFAULT 0,
    position    INT NOT NULL DEFAULT 0
);
CREATE INDEX IF NOT EXISTS idx_task_pipeline_jobs_pipeline ON task_pipeline_jobs(pipeline_id, position);
