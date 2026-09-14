-- Where each deployable part of a repository is actually hosted, as a link to
-- the provider's own object (a Vercel project today), rather than as a recipe.
--
-- repository_deploy_targets answers "which workflow ships env X and with which
-- variables"; it is keyed by environment and knows nothing about a monorepo
-- whose frontend is on Vercel while its backend runs on Cloud Run. This table
-- is keyed by AREA — '' for a single-kind repository, or the sub-repo kind
-- (frontend/backend) on a monorepo — and records the provider-side identity
-- (project id, team, root directory, production URL) so agents and the UI can
-- reach the right dashboard without re-deriving it from the tree every time.
--
-- source says who established the link: 'detected' (a .vercel/project.json or
-- a git-linked project matched the repository) or 'user' (chosen by hand when
-- detection could not decide). evidence keeps the path/reason behind it.
CREATE TABLE IF NOT EXISTS repository_hosting_links (
    id             UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    repository_id  UUID        NOT NULL REFERENCES repositories(id) ON DELETE CASCADE,
    area           TEXT        NOT NULL DEFAULT '',
    provider       TEXT        NOT NULL,
    external_id    TEXT        NOT NULL DEFAULT '',
    external_name  TEXT        NOT NULL DEFAULT '',
    scope_id       TEXT        NOT NULL DEFAULT '',
    scope_slug     TEXT        NOT NULL DEFAULT '',
    root_directory TEXT        NOT NULL DEFAULT '',
    production_url TEXT        NOT NULL DEFAULT '',
    source         TEXT        NOT NULL DEFAULT 'user',
    evidence       TEXT        NOT NULL DEFAULT '',
    created_at     TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at     TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (repository_id, area)
);
