-- migration 114 tenant-scoped every foreign key by widening it to a composite
-- (tenant_id, col) pair referencing (tenant_id, id) on the target table. That
-- was correct for the JOIN half of each constraint — it is what makes a stray
-- cross-tenant id impossible to insert — but every one of them also carried
-- "ON DELETE SET NULL" unscoped, and on a COMPOSITE foreign key that action
-- nulls EVERY column in the tuple, tenant_id included. tenant_id is NOT NULL
-- on every one of these tables, so deleting the referenced row (a board task,
-- an agent, a reflection, a session run, ...) crashed with exactly the
-- constraint violation this migration exists to stop:
--   null value in column "tenant_id" of relation "sessions" violates
--   not-null constraint (SQLSTATE 23502)
-- reported live off deleting a board task, which nulls sessions.task_id via
-- sessions_task_id_fkey and, unscoped, sessions.tenant_id with it.
--
-- PostgreSQL 15+ (this deployment runs 16 — see `gcloud sql instances list`)
-- lets ON DELETE SET NULL name exactly which columns it nulls:
-- "ON DELETE SET NULL (col)". Naming only the logically-nullable column is
-- the fix, for all twelve constraints migration 114 introduced with this
-- shape — sessions.task_id was simply the first one anybody hit.
--
-- Every ADD CONSTRAINT below revalidates existing rows against the FK, and
-- these tables carry FORCE ROW LEVEL SECURITY — even the owning migration
-- role must satisfy each table's policy, which reads
-- current_setting('app.tenant_id'). A migration is schema-wide, not scoped
-- to any one tenant's request, so that GUC has never been SET in this
-- session and current_setting throws 42704 (unrecognized configuration
-- parameter) the instant Postgres evaluates the first row's policy —
-- confirmed live: "apply migration 121...: ERROR: unrecognized
-- configuration parameter \"app.tenant_id\"" crash-looped the pod.
--
-- set_config's third argument is SET LOCAL semantics — scoped to this
-- transaction, which is the whole of this migration file (see
-- applyMigrationOnce, one tx.Exec per file). The nil UUID satisfies
-- current_setting()::uuid's parse and matches no real tenant, so RLS hides
-- every row from the validation scan, which is exactly correct: the JOIN
-- condition these constraints enforce is UNCHANGED by this migration, only
-- what ON DELETE nulls is — there is nothing new to validate against
-- existing data.
SELECT set_config('app.tenant_id', '00000000-0000-0000-0000-000000000000', true);

ALTER TABLE agent_evolution_events DROP CONSTRAINT agent_evolution_events_reflection_id_fkey;
ALTER TABLE agent_evolution_events ADD CONSTRAINT agent_evolution_events_reflection_id_fkey
    FOREIGN KEY (tenant_id, reflection_id) REFERENCES agent_reflections(tenant_id, id) ON DELETE SET NULL (reflection_id);

ALTER TABLE agent_evolution_events DROP CONSTRAINT agent_evolution_events_reverted_event_id_fkey;
ALTER TABLE agent_evolution_events ADD CONSTRAINT agent_evolution_events_reverted_event_id_fkey
    FOREIGN KEY (tenant_id, reverted_event_id) REFERENCES agent_evolution_events(tenant_id, id) ON DELETE SET NULL (reverted_event_id);

ALTER TABLE agent_golden_results DROP CONSTRAINT agent_golden_results_reflection_id_fkey;
ALTER TABLE agent_golden_results ADD CONSTRAINT agent_golden_results_reflection_id_fkey
    FOREIGN KEY (tenant_id, reflection_id) REFERENCES agent_reflections(tenant_id, id) ON DELETE SET NULL (reflection_id);

ALTER TABLE agent_score_events DROP CONSTRAINT agent_score_events_task_id_fkey;
ALTER TABLE agent_score_events ADD CONSTRAINT agent_score_events_task_id_fkey
    FOREIGN KEY (tenant_id, task_id) REFERENCES board_tasks(tenant_id, id) ON DELETE SET NULL (task_id);

ALTER TABLE board_tasks DROP CONSTRAINT board_tasks_initiative_project_id_fkey;
ALTER TABLE board_tasks ADD CONSTRAINT board_tasks_initiative_project_id_fkey
    FOREIGN KEY (tenant_id, initiative_project_id) REFERENCES projects(tenant_id, id) ON DELETE SET NULL (initiative_project_id);

ALTER TABLE board_tasks DROP CONSTRAINT project_tasks_assignee_agent_id_fkey;
ALTER TABLE board_tasks ADD CONSTRAINT project_tasks_assignee_agent_id_fkey
    FOREIGN KEY (tenant_id, assignee_agent_id) REFERENCES agents(tenant_id, id) ON DELETE SET NULL (assignee_agent_id);

ALTER TABLE ops_audit_log DROP CONSTRAINT ops_audit_log_repository_id_fkey;
ALTER TABLE ops_audit_log ADD CONSTRAINT ops_audit_log_repository_id_fkey
    FOREIGN KEY (tenant_id, repository_id) REFERENCES repositories(tenant_id, id) ON DELETE SET NULL (repository_id);

ALTER TABLE plan_tasks DROP CONSTRAINT plan_tasks_agent_id_fkey;
ALTER TABLE plan_tasks ADD CONSTRAINT plan_tasks_agent_id_fkey
    FOREIGN KEY (tenant_id, agent_id) REFERENCES agents(tenant_id, id) ON DELETE SET NULL (agent_id);

ALTER TABLE prod_incidents DROP CONSTRAINT prod_incidents_task_id_fkey;
ALTER TABLE prod_incidents ADD CONSTRAINT prod_incidents_task_id_fkey
    FOREIGN KEY (tenant_id, task_id) REFERENCES board_tasks(tenant_id, id) ON DELETE SET NULL (task_id);

ALTER TABLE sessions DROP CONSTRAINT sessions_task_id_fkey;
ALTER TABLE sessions ADD CONSTRAINT sessions_task_id_fkey
    FOREIGN KEY (tenant_id, task_id) REFERENCES board_tasks(tenant_id, id) ON DELETE SET NULL (task_id);

ALTER TABLE task_agent_runs DROP CONSTRAINT task_agent_runs_session_run_id_fkey;
ALTER TABLE task_agent_runs ADD CONSTRAINT task_agent_runs_session_run_id_fkey
    FOREIGN KEY (tenant_id, session_run_id) REFERENCES session_runs(tenant_id, id) ON DELETE SET NULL (session_run_id);

ALTER TABLE task_column_spans DROP CONSTRAINT task_column_spans_agent_id_fkey;
ALTER TABLE task_column_spans ADD CONSTRAINT task_column_spans_agent_id_fkey
    FOREIGN KEY (tenant_id, agent_id) REFERENCES agents(tenant_id, id) ON DELETE SET NULL (agent_id);

ALTER TABLE task_criterion_checks DROP CONSTRAINT task_criterion_checks_agent_id_fkey;
ALTER TABLE task_criterion_checks ADD CONSTRAINT task_criterion_checks_agent_id_fkey
    FOREIGN KEY (tenant_id, agent_id) REFERENCES agents(tenant_id, id) ON DELETE SET NULL (agent_id);
