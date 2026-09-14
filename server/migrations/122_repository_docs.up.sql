-- Per-repository (and, inside a RepoSubProject, per-sub-project) pointers to
-- the four reference docs agents should read before touching that code:
-- coding standards, test standards, architecture, and how to run it locally.
-- Same convention as sub_projects (migration 118) for the same reasons: read
-- and written whole, never queried by key, shape validated in domain before
-- it is ever persisted.
ALTER TABLE repositories
    ADD COLUMN IF NOT EXISTS docs JSONB NOT NULL DEFAULT '{}'::jsonb;
