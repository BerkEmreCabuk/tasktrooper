CREATE TABLE deployment_runs (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    repository_id   UUID NOT NULL REFERENCES repositories(id) ON DELETE CASCADE,
    env             TEXT NOT NULL,            -- stage | preprod | prod
    run_id          BIGINT NOT NULL,          -- GitHub Actions run id; the dedupe key
    run_number      INT  NOT NULL DEFAULT 0,
    workflow_file   TEXT NOT NULL DEFAULT '',
    head_sha        TEXT NOT NULL DEFAULT '',
    head_ref        TEXT NOT NULL DEFAULT '',
    status          TEXT NOT NULL,            -- queued | in_progress | completed
    conclusion      TEXT NOT NULL DEFAULT '', -- success | failure | cancelled | ''
    html_url        TEXT NOT NULL DEFAULT '',
    trigger_source  TEXT NOT NULL DEFAULT 'external',  -- ui | rollback | external
    triggered_by    TEXT NOT NULL DEFAULT '', -- tenant_uid of the actor
    rollback_of_sha TEXT NOT NULL DEFAULT '', -- non-empty ⇒ this run is a rollback
    started_at      TIMESTAMPTZ,
    completed_at    TIMESTAMPTZ,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (repository_id, run_id)
);
CREATE INDEX idx_deployment_runs_repo_env
    ON deployment_runs (repository_id, env, started_at DESC);
-- "the last good ref for this env" — rollback's only lookup.
CREATE INDEX idx_deployment_runs_good
    ON deployment_runs (repository_id, env, completed_at DESC)
    WHERE conclusion = 'success';

-- GitHub's dispatch API returns 204 with no run id, so the intent behind a
-- dispatch has to be recorded here and reconciled with the run once it appears.
CREATE TABLE deploy_dispatches (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    repository_id   UUID NOT NULL REFERENCES repositories(id) ON DELETE CASCADE,
    env             TEXT NOT NULL,
    workflow_file   TEXT NOT NULL,
    ref             TEXT NOT NULL,          -- branch or rollback tag actually dispatched
    kind            TEXT NOT NULL,          -- deploy | rollback
    rollback_of_sha TEXT NOT NULL DEFAULT '',
    actor           TEXT NOT NULL DEFAULT '',
    state           TEXT NOT NULL DEFAULT 'pending',  -- pending | matched | abandoned
    matched_run_id  BIGINT,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX idx_deploy_dispatches_pending
    ON deploy_dispatches (repository_id, env, created_at DESC) WHERE state = 'pending';

CREATE TABLE ops_audit_log (
    id            UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    repository_id UUID REFERENCES repositories(id) ON DELETE SET NULL,
    action        TEXT NOT NULL,  -- deploy | rollback | store_submit | store_release |
                                  -- store_promote | store_rollout | store_halt |
                                  -- store_resume | store_verify
    target        TEXT NOT NULL DEFAULT '',    -- env or platform
    actor         TEXT NOT NULL DEFAULT '',    -- Locals("tenant_uid"), else 'system'
    detail        JSONB NOT NULL DEFAULT '{}'::jsonb,
    outcome       TEXT NOT NULL DEFAULT 'ok',  -- ok | error
    error         TEXT NOT NULL DEFAULT '',
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX idx_ops_audit_repo ON ops_audit_log (repository_id, created_at DESC);
