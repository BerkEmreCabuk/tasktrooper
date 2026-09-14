-- Shipping and production were the two stages the board could not describe.
-- A repo's deploy existed only as a hand-written GitHub Actions workflow file
-- (the pipeline mapping points at it, but nothing records what it deploys to),
-- and production problems reached the system only through a human reading logs
-- and typing a task by hand.
--
-- repository_deploy_targets makes the deploy definable per repo per env
-- (provider + vars + the URL that proves the env is alive), and prod_incidents
-- gives alerts, probes and failed deploys a durable, deduplicated home that the
-- remedy engine and the board can both act on.

CREATE TABLE IF NOT EXISTS repository_deploy_targets (
    id            UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    repository_id UUID NOT NULL REFERENCES repositories(id) ON DELETE CASCADE,
    -- stage | preprod | prod — mirrors the pipeline deploy categories.
    env           TEXT NOT NULL,
    provider      TEXT NOT NULL,
    -- The deploy template this target was scaffolded from ('' = hand-rolled).
    template_id   TEXT NOT NULL DEFAULT '',
    vars          JSONB NOT NULL DEFAULT '{}'::jsonb,
    -- Polled by the production monitor; '' disables probing for this env.
    health_url    TEXT NOT NULL DEFAULT '',
    auto_rollback BOOLEAN NOT NULL DEFAULT false,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (repository_id, env)
);

-- What a production incident on this repo is allowed to trigger:
-- off | suggest (open a diagnosis task that stops at a proposal) | auto_fix.
ALTER TABLE repositories
    ADD COLUMN IF NOT EXISTS incident_policy TEXT NOT NULL DEFAULT 'suggest';

CREATE TABLE IF NOT EXISTS prod_incidents (
    id            UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    repository_id UUID NOT NULL REFERENCES repositories(id) ON DELETE CASCADE,
    env           TEXT NOT NULL DEFAULT 'prod',
    -- webhook | probe | deploy | manual
    source        TEXT NOT NULL,
    fingerprint   TEXT NOT NULL,
    title         TEXT NOT NULL,
    detail        TEXT NOT NULL DEFAULT '',
    severity      TEXT NOT NULL DEFAULT 'medium',
    status        TEXT NOT NULL DEFAULT 'open',
    payload       JSONB NOT NULL DEFAULT '{}'::jsonb,
    -- The current proposal: rules-derived at ingest, replaced by the agent's
    -- diagnosis when it finishes.
    remedy        TEXT NOT NULL DEFAULT '',
    remedy_kind   TEXT NOT NULL DEFAULT '',
    confidence    INT NOT NULL DEFAULT 0,
    occurrences   INT NOT NULL DEFAULT 1,
    task_id       UUID REFERENCES board_tasks(id) ON DELETE SET NULL,
    first_seen_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    last_seen_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    resolved_at   TIMESTAMPTZ
);

-- Dedupe only against live incidents: the same alert firing again months after
-- it was resolved is a new incident, not a 400th occurrence of the old one.
CREATE UNIQUE INDEX IF NOT EXISTS idx_prod_incidents_live_fingerprint
    ON prod_incidents (repository_id, env, fingerprint)
    WHERE status NOT IN ('resolved', 'ignored');

CREATE INDEX IF NOT EXISTS idx_prod_incidents_repo_status
    ON prod_incidents (repository_id, status, last_seen_at DESC);
-- History lookup for the remedy engine: "did this exact fingerprint happen
-- before, and what actually fixed it?"
CREATE INDEX IF NOT EXISTS idx_prod_incidents_history
    ON prod_incidents (repository_id, fingerprint, resolved_at DESC);
CREATE INDEX IF NOT EXISTS idx_prod_incidents_task
    ON prod_incidents (task_id) WHERE task_id IS NOT NULL;

CREATE TABLE IF NOT EXISTS prod_incident_events (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    incident_id UUID NOT NULL REFERENCES prod_incidents(id) ON DELETE CASCADE,
    kind        TEXT NOT NULL,
    message     TEXT NOT NULL DEFAULT '',
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_prod_incident_events_incident
    ON prod_incident_events (incident_id, created_at);
