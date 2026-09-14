-- Google Cloud as a connected provider: one service-account credential per
-- tenant, and a per-(repository, sub-project) binding to one resource inside
-- the project that credential reaches.
--
-- set_config first, and it is load-bearing for the same reason migrations 121
-- and 124 needed it: the tables below carry FORCE ROW LEVEL SECURITY and a
-- tenant_id DEFAULT current_setting('app.tenant_id'), while a migration is
-- schema-wide with no tenant in it. Same nil uuid, SET LOCAL semantics, so it
-- dies with this transaction.
SELECT set_config('app.tenant_id', '00000000-0000-0000-0000-000000000000', true);

-- One row per tenant, not one per provider: Google Cloud is reached with a
-- single service account, and the routes that manage it are singular
-- (/v1/gcloud/credential) rather than keyed the way store_credentials is.
--
-- project_id and client_email sit in PLAINTEXT columns beside the encrypted
-- blob on purpose. They are identifiers, not key material — the console has to
-- render "connected as <sa>@<project>.iam.gserviceaccount.com" without
-- decrypting anything, and the resource listing needs the project id on every
-- call. The private key never leaves `data`, which is AES-GCM ciphertext
-- written by domain/secrets.Cipher exactly like store_credentials.data is.
CREATE TABLE IF NOT EXISTS gcloud_credentials (
    tenant_id    UUID        PRIMARY KEY DEFAULT current_setting('app.tenant_id')::uuid,
    project_id   TEXT        NOT NULL DEFAULT '',
    client_email TEXT        NOT NULL DEFAULT '',
    data         BYTEA       NOT NULL,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at   TIMESTAMPTZ NOT NULL DEFAULT now()
);

ALTER TABLE gcloud_credentials ENABLE ROW LEVEL SECURITY;
ALTER TABLE gcloud_credentials FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON gcloud_credentials
    USING (tenant_id = current_setting('app.tenant_id')::uuid)
    WITH CHECK (tenant_id = current_setting('app.tenant_id')::uuid);

-- Which Google Cloud resource each deployable part of a repository IS.
--
-- Keyed by (repository, sub_project_path) rather than by the area vocabulary
-- repository_hosting_links uses (migration 112): that table answers "where is
-- this hosted on Vercel" for a whole sub-repo KIND, while a monorepo can put
-- its backend and its worker in two different Cloud Run services that share
-- the 'backend' kind. sub_project_path is the addressable key sub_projects
-- (migration 118) already established, and '' is the repository itself.
--
-- A separate table rather than more keys inside the sub_projects JSONB: that
-- column is read and written WHOLE by the repository PATCH path, so a binding
-- stored there would be silently erased by any concurrent edit of the
-- sub-project list — and unlike detected_build_targets, a binding is written
-- by a different endpoint than the one that rewrites sub_projects.
--
-- resource_name is the FULLY QUALIFIED GCP name
-- (projects/<p>/locations/<loc>/services/<svc>), never the short one: it is
-- the only form that stays unambiguous across regions, and it is what the
-- detail read is issued against.
CREATE TABLE IF NOT EXISTS repository_gcloud_resources (
    id               UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id        UUID        NOT NULL DEFAULT current_setting('app.tenant_id')::uuid,
    repository_id    UUID        NOT NULL,
    sub_project_path TEXT        NOT NULL DEFAULT '',
    resource_type    TEXT        NOT NULL CHECK (resource_type IN ('cloud_run', 'gke_cluster')),
    resource_name    TEXT        NOT NULL,
    display_name     TEXT        NOT NULL DEFAULT '',
    project_id       TEXT        NOT NULL DEFAULT '',
    location         TEXT        NOT NULL DEFAULT '',
    source           TEXT        NOT NULL DEFAULT 'user',
    created_at       TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at       TIMESTAMPTZ NOT NULL DEFAULT now(),
    -- Composite FK, per migration 121. CASCADE takes the whole row so it needs
    -- no column list, but the referencing tuple still has to carry tenant_id:
    -- a single-column reference to repositories(id) would let a row point at
    -- another tenant's repository, which is exactly what RLS cannot see.
    CONSTRAINT repository_gcloud_resources_repository_id_fkey
        FOREIGN KEY (tenant_id, repository_id) REFERENCES repositories(tenant_id, id) ON DELETE CASCADE,
    CONSTRAINT repository_gcloud_resources_tenant_key UNIQUE (tenant_id, id),
    CONSTRAINT repository_gcloud_resources_scope_key UNIQUE (tenant_id, repository_id, sub_project_path)
);

ALTER TABLE repository_gcloud_resources ENABLE ROW LEVEL SECURITY;
ALTER TABLE repository_gcloud_resources FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON repository_gcloud_resources
    USING (tenant_id = current_setting('app.tenant_id')::uuid)
    WITH CHECK (tenant_id = current_setting('app.tenant_id')::uuid);

CREATE INDEX IF NOT EXISTS idx_repository_gcloud_resources_repository
    ON repository_gcloud_resources(tenant_id, repository_id, sub_project_path);
