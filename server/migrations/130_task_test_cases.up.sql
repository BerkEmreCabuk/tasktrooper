-- Test cases a QA round derived, executed and judged, kept on the task the way
-- acceptance criteria are.
--
-- The criteria say what was ASKED FOR; they are written before the work and
-- they are deliberately few. A test round is the other list: every case the
-- requirement implies — boundaries, negative input, auth, empty state,
-- regression — including the ones that were considered and thrown away. Until
-- now that list lived inside the QA run's own transcript, so the board could
-- show that a criterion passed but never WHAT was tried, what failed, or which
-- case was rejected as not applicable. A round that tested three things and one
-- that tested thirty looked identical on the card.
--
-- `status` carries the three answers a reader is looking for: passed, failed,
-- and invalid — a case that was thought of and is NOT a valid case (out of
-- scope, contradicts the spec, unreachable by design), with the reason in
-- `notes`. `planned` is the case that was written down but not yet executed,
-- and `skipped` the one that could not be executed at all (no device, no stage
-- deploy), which is never the same claim as passing.
-- set_config for the reason migrations 121 and 124 give: a migration is
-- schema-wide and has no tenant in it, so anything that makes Postgres evaluate
-- a tenant policy would throw 42704 on current_setting. SET LOCAL semantics —
-- it dies with this transaction.
SELECT set_config('app.tenant_id', '00000000-0000-0000-0000-000000000000', true);

-- Both foreign keys are (tenant_id, col) → parent(tenant_id, id), the shape
-- migration 114 gave every cross-tenant reference: a single-column key is
-- enforced against EVERY tenant's rows, so one tenant could point at another's
-- task and the other's delete would then reach across. The criterion key names
-- its nulled column ("SET NULL (criterion_id)") for migration 121's reason: an
-- unqualified SET NULL on a composite key nulls tenant_id too, which is NOT
-- NULL, so deleting a criterion would fail with 23502.
CREATE TABLE IF NOT EXISTS task_test_cases (
    id           UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id    UUID NOT NULL DEFAULT current_setting('app.tenant_id')::uuid,
    task_id      UUID NOT NULL,
    -- Nullable on purpose: the cases that matter most are the ones NO criterion
    -- spelled out. ON DELETE SET NULL because update_board_task replaces the
    -- whole criteria list, and an executed test case must not vanish with the
    -- criterion row it happened to be linked to.
    criterion_id UUID,
    title        TEXT NOT NULL,
    category     TEXT NOT NULL DEFAULT 'happy_path'
                 CHECK (category IN ('happy_path', 'boundary', 'negative', 'auth',
                                     'empty_state', 'regression', 'visual', 'async', 'other')),
    status       TEXT NOT NULL DEFAULT 'planned'
                 CHECK (status IN ('planned', 'passed', 'failed', 'skipped', 'invalid')),
    expected     TEXT NOT NULL DEFAULT '',
    actual       TEXT NOT NULL DEFAULT '',
    evidence     TEXT NOT NULL DEFAULT '',
    notes        TEXT NOT NULL DEFAULT '',
    position     INT NOT NULL DEFAULT 0,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT task_test_cases_task_id_fkey
        FOREIGN KEY (tenant_id, task_id) REFERENCES board_tasks(tenant_id, id) ON DELETE CASCADE,
    CONSTRAINT task_test_cases_criterion_id_fkey
        FOREIGN KEY (tenant_id, criterion_id) REFERENCES task_acceptance_criteria(tenant_id, id)
        ON DELETE SET NULL (criterion_id)
);

CREATE INDEX IF NOT EXISTS idx_task_test_cases_task ON task_test_cases(task_id, position);
-- Upserting by title is how a round updates the case it just executed without
-- first reading back an id it never held.
CREATE UNIQUE INDEX IF NOT EXISTS idx_task_test_cases_task_title ON task_test_cases(tenant_id, task_id, title);

ALTER TABLE task_test_cases ENABLE ROW LEVEL SECURITY;
ALTER TABLE task_test_cases FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON task_test_cases
    USING (tenant_id = current_setting('app.tenant_id')::uuid)
    WITH CHECK (tenant_id = current_setting('app.tenant_id')::uuid);

-- No tool_policy backfill here, unlike migration 073's. Since migration 114
-- every tenant-scoped table carries FORCE ROW LEVEL SECURITY and the migration
-- runner is a NOSUPERUSER NOBYPASSRLS owner with no tenant in scope, so an
-- UPDATE over `agents` sees zero rows however it is written — it would look
-- like a backfill and do nothing. The new tools reach existing installs from
-- catalog.grantMissingRoleTools, which runs per tenant at boot where the tenant
-- context exists.
