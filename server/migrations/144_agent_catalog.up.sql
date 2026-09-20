-- External agents & skills catalog (release B follow-up).
--
-- Agent and skill definitions now come from a separate, externally editable
-- catalog (a git repo or a local directory) that the app watches and pulls at
-- boot, on an interval, and from a manual button — see application/catalog/
-- catalogsync.go and the ingest adapter in adapter/catalogrepo. The schema here
-- is the reconciliation ledger between the catalog and the live rows:
-- agents.catalog_slug keys each agent to its definition, catalog_etag is the
-- hash of the revision applied, the two toggles are the per-agent gates the
-- sync must not cross, skills.catalog_sha is the revision marker per applied
-- skill, and catalog_pending parks upstream changes the gates or a failed LLM
-- merge kept the sync from applying.

ALTER TABLE agents
    ADD COLUMN catalog_slug text CHECK (catalog_slug = '' OR catalog_slug = lower(catalog_slug)),
    ADD COLUMN catalog_etag text NOT NULL DEFAULT '',
    ADD COLUMN auto_pull_agent_updates boolean NOT NULL DEFAULT true,
    ADD COLUMN keep_skills_updated boolean NOT NULL DEFAULT true;

ALTER TABLE skills
    ADD COLUMN catalog_sha text NOT NULL DEFAULT '';

CREATE INDEX idx_agents_catalog_slug ON agents (catalog_slug) WHERE catalog_slug <> '';

CREATE TABLE catalog_sync_state (
    id smallint PRIMARY KEY CHECK (id = 1),
    repo_ref text NOT NULL DEFAULT '',
    last_sync_at timestamptz,
    last_error text NOT NULL DEFAULT '',
    last_summary jsonb,
    pending_count int NOT NULL DEFAULT 0,
    updated_at timestamptz NOT NULL DEFAULT now()
);

-- One row, seeded so a status read never has to decide "no row yet".
INSERT INTO catalog_sync_state (id) VALUES (1);

CREATE TABLE catalog_pending (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    agent_slug text NOT NULL,
    agent_name text NOT NULL DEFAULT '',
    kind text NOT NULL CHECK (kind IN ('agent', 'skill')),
    name text NOT NULL,
    action text NOT NULL CHECK (action IN ('create', 'update', 'delete', 'merge')),
    reason text NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX idx_catalog_pending_key ON catalog_pending (agent_slug, kind, name) WHERE created_at IS NOT NULL;