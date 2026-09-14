-- Skills and rules were mutated in place: self-evolution rewrote a skill and
-- the previous wording was gone. Reverting only worked while the evolution
-- event that made the change was still around, and only for that one step.
--
-- Every write to a skill or rule now appends an immutable row here, so any
-- earlier wording can be restored by version number regardless of who wrote
-- it (a user edit, a reflection, or a seed sync).
CREATE TABLE IF NOT EXISTS catalog_versions (
    id            UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    agent_id      UUID NOT NULL,
    target_kind   TEXT NOT NULL,               -- skill | rule
    target_id     UUID NOT NULL,
    version       INT  NOT NULL,               -- 1-based, per target
    action        TEXT NOT NULL,               -- create | update | delete | restore
    name          TEXT NOT NULL DEFAULT '',
    description   TEXT NOT NULL DEFAULT '',
    category      TEXT NOT NULL DEFAULT '',
    tags          TEXT[] NOT NULL DEFAULT '{}',
    content       TEXT NOT NULL DEFAULT '',
    priority      INT  NOT NULL DEFAULT 0,
    enabled       BOOLEAN NOT NULL DEFAULT true,
    source        TEXT NOT NULL DEFAULT 'user', -- user | evolution | seed | restore
    reason        TEXT NOT NULL DEFAULT '',
    reflection_id UUID,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (target_kind, target_id, version)
);

CREATE INDEX IF NOT EXISTS idx_catalog_versions_target
    ON catalog_versions(target_kind, target_id, version DESC);
CREATE INDEX IF NOT EXISTS idx_catalog_versions_agent
    ON catalog_versions(agent_id, created_at DESC);
