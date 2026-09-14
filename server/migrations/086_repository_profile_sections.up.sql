-- The project profile becomes sectioned, evidence-bearing and per-fact fresh.
--
-- It used to be one markdown blob on repositories.profile_md, written wholesale
-- by a model from a prose prompt. That shape had three failures baked in:
-- the model answered fact questions (build command, deploy shape) from its
-- priors instead of from the tree, nothing could be re-verified, and a push
-- that touched one app invalidated the whole profile or nothing at all.
--
-- Sections fix all three. Derived sections are written by a parser
-- (application/repofacts) and are never handed to a model. Agent sections
-- carry evidence paths that are validated against the working copy before the
-- write is accepted. Every section records the files its truth depends on, so
-- a push marks stale exactly the sections whose sources moved.
--
-- repositories.profile_md stays as the rendered cache: existing injection
-- sites and the settings UI keep reading it, and it is regenerated from the
-- sections on every write.
CREATE TABLE IF NOT EXISTS repository_profile_sections (
    id            UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    repository_id UUID NOT NULL REFERENCES repositories(id) ON DELETE CASCADE,
    section       TEXT NOT NULL,
    body_md       TEXT NOT NULL,
    -- [{path, line, note}] — the paths a reader (or the next refresh) can
    -- check the section against.
    evidence      JSONB NOT NULL DEFAULT '[]'::jsonb,
    -- The files this section's truth depends on. A push touching any of them
    -- marks the section stale; a push touching none leaves it alone.
    source_paths  TEXT[] NOT NULL DEFAULT '{}',
    source_commit TEXT NOT NULL DEFAULT '',
    -- 'derived' (parser-owned, rewritten every refresh) or 'agent' (judgment,
    -- survives until its evidence goes stale).
    origin        TEXT NOT NULL DEFAULT 'agent',
    stale         BOOLEAN NOT NULL DEFAULT FALSE,
    updated_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (repository_id, section)
);

CREATE INDEX IF NOT EXISTS idx_repo_profile_sections_repo
    ON repository_profile_sections (repository_id);

-- Settings the profiling pass can answer for itself: repo kind, sub-projects,
-- build/test/verify commands, which workflow fills a pipeline slot. Applied
-- automatically when the target field is still empty, offered for one click
-- when applying would overwrite a human's choice.
--
-- slot separates the several pipeline-job proposals a repo can carry at once
-- (one per category × sub-repo kind); it is '' for single-valued fields, which
-- is what makes the unique key a per-refresh replace rather than an append.
CREATE TABLE IF NOT EXISTS repository_profile_proposals (
    id            UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    repository_id UUID NOT NULL REFERENCES repositories(id) ON DELETE CASCADE,
    field         TEXT NOT NULL,
    slot          TEXT NOT NULL DEFAULT '',
    value         JSONB NOT NULL,
    current_value TEXT NOT NULL DEFAULT '',
    label         TEXT NOT NULL DEFAULT '',
    evidence      JSONB NOT NULL DEFAULT '[]'::jsonb,
    status        TEXT NOT NULL DEFAULT 'pending',
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    applied_at    TIMESTAMPTZ,
    UNIQUE (repository_id, field, slot)
);

CREATE INDEX IF NOT EXISTS idx_repo_profile_proposals_repo_status
    ON repository_profile_proposals (repository_id, status);
