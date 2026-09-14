-- Teams: who is in this tenant, whose task it is, and whose memory it is.
--
-- Until now a tenant was one person. `board_events.actor_user_id` (113) was
-- the first column that admitted otherwise; it recorded WHICH human, but there
-- was nothing to join it to, no way to offer a list of people to assign to,
-- and no notion of a memory belonging to one of them.

-- ---------------------------------------------------------------------------
-- tenants - the local registry
-- ---------------------------------------------------------------------------
--
-- GLOBAL, and the second of only two tables that are (schema_migrations is the
-- other). It is the INDEX of tenants, not one tenant's data: nothing here
-- belongs to a tenant, and giving it a tenant_id policy would make it
-- unreadable in exactly the two situations it exists for -
--
--   1. `postgres.DB.EachTenant`, which fans a fleet-wide background sweep
--      (reconciler, quota/deploy/work-order sweepers, the store and health
--      monitors) out over one tenant-scoped transaction each. Before this
--      table those loops simply queried "all rows", which a shared database
--      cannot answer.
--   2. `tenantboot`, which needs to know whether this tenant has been seeded
--      before it can scope anything to it.
--
-- It deliberately holds no personal data - no name, no email, nothing but the
-- uuid the control plane signs and two timestamps. Everything a human-readable
-- roster needs is in tenant_members, which IS policy-protected.
--
-- The control plane (tenant-manager) is the source of truth for which tenants
-- exist; this is a cache of the ones this database has actually served, filled
-- in on first sight of a signed X-Internal-Tenant header. A tenant that has
-- never made a request has no row and no rows to sweep, which is correct.
CREATE TABLE IF NOT EXISTS tenants (
    id UUID PRIMARY KEY,
    first_seen_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    -- NULL until tenantboot has seeded the board. It is a timestamp rather
    -- than a boolean so a re-seed after a schema change can be told from a
    -- tenant that was never seeded at all.
    bootstrapped_at TIMESTAMPTZ
);

-- ---------------------------------------------------------------------------
-- tenant_members - a mirror, not a registry
-- ---------------------------------------------------------------------------
--
-- The roster lives in the control plane, which owns invites, roles and
-- removal. This copy exists so the board can render "Ayşe" instead of a
-- Firebase uid and can refuse an assignee who is not in the tenant, without a
-- cross-service call on every card.
--
-- It is refreshed from the SIGNED identity headers on each request: the caller
-- upserts themselves (uid from X-Internal-Actor, role from X-Internal-Role).
-- That means it is eventually complete rather than immediately complete - a
-- member who has never opened the app is not in it yet - and it is never
-- authoritative: role checks are made against the header on the request being
-- served, never against this table, so a role revoked in the control plane
-- takes effect on the next request rather than whenever this mirror catches
-- up.
--
-- user_id is TEXT because a Firebase uid is, matching actor_user_id (113).
CREATE TABLE IF NOT EXISTS tenant_members (
    tenant_id UUID NOT NULL DEFAULT current_setting('app.tenant_id')::uuid,
    user_id TEXT NOT NULL,
    email TEXT NOT NULL DEFAULT '',
    display_name TEXT NOT NULL DEFAULT '',
    role TEXT NOT NULL DEFAULT 'member' CHECK (role IN ('owner', 'admin', 'member')),
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (tenant_id, user_id)
);

ALTER TABLE tenant_members ENABLE ROW LEVEL SECURITY;
ALTER TABLE tenant_members FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON tenant_members
    USING (tenant_id = current_setting('app.tenant_id')::uuid)
    WITH CHECK (tenant_id = current_setting('app.tenant_id')::uuid);

-- ---------------------------------------------------------------------------
-- ownership and assignment
-- ---------------------------------------------------------------------------

-- NULL means a shared/team agent - one every member may use and whose memories
-- everybody sees. A solo tenant's agents are all NULL, which is what makes a
-- solo tenant behave exactly as it did before this migration: every "is this
-- mine?" test is written as "mine OR shared", and with nothing owned, that is
-- "everything".
ALTER TABLE agents ADD COLUMN IF NOT EXISTS owner_user_id TEXT;

-- The person a card belongs to, distinct from board_tasks.assignee_agent_id,
-- which is the AGENT working it. Nullable: an unassigned card is the normal
-- state of a backlog, and on a solo tenant it is the only state.
--
-- Not a foreign key to tenant_members: the mirror is eventually complete, so a
-- FK would refuse to assign a teammate who has been invited in the control
-- plane but has not opened the app yet. Validation is a lookup at the point of
-- assignment, where a helpful error can be returned, rather than a 23503 from
-- the driver.
ALTER TABLE board_tasks ADD COLUMN IF NOT EXISTS assignee_user_id TEXT;

-- Migration 065 gave memories four buckets from two nullable dimensions:
-- agent_id NULL = the team's, repository_id NULL = valid everywhere. This adds
-- a third dimension on the same principle rather than a parallel scheme:
--
--   owner_user_id IS NULL  -> the tenant's, everyone reads it
--   owner_user_id = <uid>  -> that member's, only they read it
--
-- The existing convention is kept intact: a row with agent_id IS NULL is still
-- the team memory bucket, and a team memory is never owned (the read paths in
-- internal/application/memory ask for "mine OR unowned", so an owned team
-- memory would simply be a private note that no agent scope can reach).
ALTER TABLE agent_memories ADD COLUMN IF NOT EXISTS owner_user_id TEXT;

-- Partial, because the interesting question is always "which of these are
-- owned by me", and on a solo tenant every row is NULL - a full index would be
-- one dead entry per row.
CREATE INDEX IF NOT EXISTS idx_agents_owner
    ON agents(tenant_id, owner_user_id) WHERE owner_user_id IS NOT NULL;
CREATE INDEX IF NOT EXISTS idx_board_tasks_assignee_user
    ON board_tasks(tenant_id, assignee_user_id) WHERE assignee_user_id IS NOT NULL;
CREATE INDEX IF NOT EXISTS idx_agent_memories_owner
    ON agent_memories(tenant_id, owner_user_id, created_at DESC) WHERE owner_user_id IS NOT NULL;
