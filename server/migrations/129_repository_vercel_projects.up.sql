-- Which Vercel project each part of a repository IS, keyed by sub-project path.
--
-- repository_hosting_links (migration 112) already answers a question that
-- sounds identical, and does not answer this one: it is keyed by AREA — '' for
-- a single-kind repository, or the sub-repo kind (frontend/backend) on a
-- monorepo. A monorepo with web/ and admin/ has ONE frontend area and TWO
-- Vercel projects, so the second project has nowhere to be recorded. This
-- table is keyed by sub_project_path — the same key repositories.sub_projects
-- (migration 118) and repository_deploy_targets already use — so every project
-- the setup dialog curated can be bound to its own Vercel project.
--
-- '' is the repository as a whole and is a real key, not a missing one. Note
-- that '' and '.' are DIFFERENT rows: '.' is a legitimate sub-project path (a
-- Go module at the root beside web/), which domain.RepoSubProject documents.
--
-- A table and not another JSONB column on repositories, unlike migrations 122,
-- 125 and 127: those carry values DETECTED from a working copy and are read
-- and written whole with the rest of the repository row. This is a chosen
-- binding with its own lifecycle — created by a link click, replaced by
-- another, removed on unlink — and putting it inside sub_projects would put it
-- at the mercy of the setup dialog's whole-list PATCH, which rebuilds every
-- RepoSubProject field by field (domain.ValidateSubProjects) and would silently
-- lose a link nobody meant to remove.
--
-- project_id is Vercel's own id (prj_…) and is what every later call is
-- addressed with; the rest are the facts read off the project at link time,
-- cached so the panel renders before the live read comes back and still
-- renders when it fails. Vercel stays the source of truth for all of them.
-- team_id '' is the token owner's personal account — again a real scope, not
-- an unset one.
--
-- tenant_id, the composite foreign key, the tenant-leading UNIQUE key and the
-- policy below are not optional decoration: every one of those four shapes is
-- asserted for EVERY table by postgres.TenantIsolationSuite (migration 114).
CREATE TABLE IF NOT EXISTS repository_vercel_projects (
    id               UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id        UUID        NOT NULL DEFAULT current_setting('app.tenant_id')::uuid,
    repository_id    UUID        NOT NULL,
    sub_project_path TEXT        NOT NULL DEFAULT '',
    project_id       TEXT        NOT NULL,
    project_name     TEXT        NOT NULL DEFAULT '',
    team_id          TEXT        NOT NULL DEFAULT '',
    team_slug        TEXT        NOT NULL DEFAULT '',
    framework        TEXT        NOT NULL DEFAULT '',
    root_directory   TEXT        NOT NULL DEFAULT '',
    production_url   TEXT        NOT NULL DEFAULT '',
    created_at       TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at       TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT repository_vercel_projects_repository_id_fkey
        FOREIGN KEY (tenant_id, repository_id)
        REFERENCES repositories (tenant_id, id) ON DELETE CASCADE,
    CONSTRAINT repository_vercel_projects_repository_id_sub_project_path_key
        UNIQUE (tenant_id, repository_id, sub_project_path)
);

ALTER TABLE repository_vercel_projects ENABLE ROW LEVEL SECURITY;
ALTER TABLE repository_vercel_projects FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON repository_vercel_projects
    USING (tenant_id = current_setting('app.tenant_id')::uuid)
    WITH CHECK (tenant_id = current_setting('app.tenant_id')::uuid);
