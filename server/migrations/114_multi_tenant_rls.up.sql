-- One agent-server process, one Postgres, every tenant. Until this migration a
-- tenant WAS a database: `team_<uid>`, its own pod, its own pool. The pod and
-- the per-tenant database are gone, so the row has to carry what the database
-- name used to.
--
-- Three pieces, and each is load-bearing on its own:
--
-- 1. `tenant_id UUID NOT NULL DEFAULT current_setting('app.tenant_id')::uuid`
--    on every tenant-scoped table. **The DEFAULT is the whole trick.** 469
--    store methods write these tables and not one of them passes a tenant: the
--    value comes off the session GUC the transaction already set
--    (`postgres.DB`, `SET LOCAL app.tenant_id`), so an INSERT that names no
--    tenant still lands in the right one, and an INSERT with no GUC set fails
--    with "unrecognized configuration parameter app.tenant_id" rather than
--    landing somewhere. There is no code path that can write a row belonging
--    to nobody.
--
-- 2. `ENABLE` **and** `FORCE` ROW LEVEL SECURITY. ENABLE alone exempts the
--    table OWNER, and the owner is exactly who the application connects as
--    (it ran these migrations) — so without FORCE the policy would guard
--    every role except the only one that matters. Note the one role RLS can
--    still not be forced onto: a SUPERUSER or a role with BYPASSRLS bypasses
--    policies unconditionally. **The application role must be neither.**
--
-- 3. Every UNIQUE key re-cut to lead with `tenant_id`. This is the half that
--    is invisible until tenant two arrives: `agents.name`, `skills.name`,
--    `board_columns.slug`, `repositories.root_path`, `api_keys.name`,
--    `board_task_counters.task_type` and the rest were unique across the
--    INSTALL, which was one tenant. Left alone, tenant two cannot create an
--    agent called "Developer" because tenant one already did. Every UNIQUE
--    constraint, every unique index and every natural-key PRIMARY KEY below
--    was found by asking the live catalog, not by memory.
--
--    Constraints already keyed on a tenant-scoped FK (agent_id, repository_id,
--    task_id …) are rewritten too. They were not broken, but a reader should
--    not have to work out which ones were: after this migration EVERY unique
--    key in this schema starts with tenant_id, and that is greppable.
--
-- Single-row-per-install becomes single-row-per-tenant: `board_settings` and
-- `billing_plan` keep their `CHECK (id = 1)` and gain `tenant_id` in the
-- PRIMARY KEY; `agent_cli_connection` keeps its `CHECK (singleton)` the same
-- way.
--
-- **There is no backfill here, and none is missing.** The shared database
-- starts EMPTY — existing per-tenant databases are not migrated into it, they
-- are abandoned (see the rework plan). That is what lets this be a plain
-- forward migration instead of an add-nullable / backfill / set-not-null
-- dance.
--
-- `SET LOCAL app.tenant_id` below is not decoration either: `ALTER TABLE ADD
-- COLUMN ... NOT NULL DEFAULT <expr>` evaluates the expression once, at
-- statement time, to stamp the pre-existing rows. With the GUC unset the very
-- first ALTER would fail. SET LOCAL, so it dies with this transaction.
--
-- Which tables are NOT tenant-scoped, and why:
--
--   schema_migrations — the schema is one schema. There is one set of tables
--     for the whole fleet, so there is one ledger of which migrations built
--     it. A per-tenant copy would claim each tenant can be at a different
--     version of a schema they physically share.
--
-- That is the complete list. Everything else was considered and rejected as
-- global, including the two that look like product catalogs:
--
--   model_prices — looks like a shipped price sheet, but `PUT/DELETE
--     /admin/billing/model-prices` lets a tenant edit it. Global would make
--     one tenant's admin rewrite everybody's cost accounting.
--   agent_templates — ships with `built_in`, but tenants create their own
--     (`AgentTemplateStore.Create/Delete`), and a template carries prompts and
--     tool policy. Global would leak one tenant's prompt library to the fleet.
--
-- One thing worth knowing before debugging a policy: a statement with no
-- app.tenant_id fails in TWO different ways depending on the connection's
-- history. A connection that has never been scoped raises `unrecognized
-- configuration parameter`; one that has served a tenant reverts SET LOCAL to
-- the parameter's SESSION value, which is the empty string, so the cast fails
-- with `invalid input syntax for type uuid: ""` instead. Both are hard errors
-- and neither returns rows, which is the property that matters - but the
-- second message does not mention the parameter, and reads at first glance
-- like a bad request rather than a missing tenant.

SET LOCAL app.tenant_id = '00000000-0000-0000-0000-000000000000';

-- ---------------------------------------------------------------------------
-- 1. the column
-- ---------------------------------------------------------------------------
ALTER TABLE agent_cli_connection ADD COLUMN tenant_id UUID NOT NULL DEFAULT current_setting('app.tenant_id')::uuid;
ALTER TABLE agent_column_subscriptions ADD COLUMN tenant_id UUID NOT NULL DEFAULT current_setting('app.tenant_id')::uuid;
ALTER TABLE agent_evolution_events ADD COLUMN tenant_id UUID NOT NULL DEFAULT current_setting('app.tenant_id')::uuid;
ALTER TABLE agent_golden_results ADD COLUMN tenant_id UUID NOT NULL DEFAULT current_setting('app.tenant_id')::uuid;
ALTER TABLE agent_golden_tasks ADD COLUMN tenant_id UUID NOT NULL DEFAULT current_setting('app.tenant_id')::uuid;
ALTER TABLE agent_kpi_results ADD COLUMN tenant_id UUID NOT NULL DEFAULT current_setting('app.tenant_id')::uuid;
ALTER TABLE agent_kpis ADD COLUMN tenant_id UUID NOT NULL DEFAULT current_setting('app.tenant_id')::uuid;
ALTER TABLE agent_memories ADD COLUMN tenant_id UUID NOT NULL DEFAULT current_setting('app.tenant_id')::uuid;
ALTER TABLE agent_performance_scores ADD COLUMN tenant_id UUID NOT NULL DEFAULT current_setting('app.tenant_id')::uuid;
ALTER TABLE agent_reflections ADD COLUMN tenant_id UUID NOT NULL DEFAULT current_setting('app.tenant_id')::uuid;
ALTER TABLE agent_score_events ADD COLUMN tenant_id UUID NOT NULL DEFAULT current_setting('app.tenant_id')::uuid;
ALTER TABLE agent_templates ADD COLUMN tenant_id UUID NOT NULL DEFAULT current_setting('app.tenant_id')::uuid;
ALTER TABLE agents ADD COLUMN tenant_id UUID NOT NULL DEFAULT current_setting('app.tenant_id')::uuid;
ALTER TABLE api_keys ADD COLUMN tenant_id UUID NOT NULL DEFAULT current_setting('app.tenant_id')::uuid;
ALTER TABLE app_settings ADD COLUMN tenant_id UUID NOT NULL DEFAULT current_setting('app.tenant_id')::uuid;
ALTER TABLE attachments ADD COLUMN tenant_id UUID NOT NULL DEFAULT current_setting('app.tenant_id')::uuid;
ALTER TABLE audit_logs ADD COLUMN tenant_id UUID NOT NULL DEFAULT current_setting('app.tenant_id')::uuid;
ALTER TABLE billing_plan ADD COLUMN tenant_id UUID NOT NULL DEFAULT current_setting('app.tenant_id')::uuid;
ALTER TABLE board_column_transitions ADD COLUMN tenant_id UUID NOT NULL DEFAULT current_setting('app.tenant_id')::uuid;
ALTER TABLE board_columns ADD COLUMN tenant_id UUID NOT NULL DEFAULT current_setting('app.tenant_id')::uuid;
ALTER TABLE board_events ADD COLUMN tenant_id UUID NOT NULL DEFAULT current_setting('app.tenant_id')::uuid;
ALTER TABLE board_members ADD COLUMN tenant_id UUID NOT NULL DEFAULT current_setting('app.tenant_id')::uuid;
ALTER TABLE board_settings ADD COLUMN tenant_id UUID NOT NULL DEFAULT current_setting('app.tenant_id')::uuid;
ALTER TABLE board_task_counters ADD COLUMN tenant_id UUID NOT NULL DEFAULT current_setting('app.tenant_id')::uuid;
ALTER TABLE board_tasks ADD COLUMN tenant_id UUID NOT NULL DEFAULT current_setting('app.tenant_id')::uuid;
ALTER TABLE catalog_versions ADD COLUMN tenant_id UUID NOT NULL DEFAULT current_setting('app.tenant_id')::uuid;
ALTER TABLE deploy_dispatches ADD COLUMN tenant_id UUID NOT NULL DEFAULT current_setting('app.tenant_id')::uuid;
ALTER TABLE deploy_package_tasks ADD COLUMN tenant_id UUID NOT NULL DEFAULT current_setting('app.tenant_id')::uuid;
ALTER TABLE deploy_packages ADD COLUMN tenant_id UUID NOT NULL DEFAULT current_setting('app.tenant_id')::uuid;
ALTER TABLE deployment_runs ADD COLUMN tenant_id UUID NOT NULL DEFAULT current_setting('app.tenant_id')::uuid;
ALTER TABLE file_chunks ADD COLUMN tenant_id UUID NOT NULL DEFAULT current_setting('app.tenant_id')::uuid;
ALTER TABLE files ADD COLUMN tenant_id UUID NOT NULL DEFAULT current_setting('app.tenant_id')::uuid;
ALTER TABLE jobs ADD COLUMN tenant_id UUID NOT NULL DEFAULT current_setting('app.tenant_id')::uuid;
ALTER TABLE live_activity_tokens ADD COLUMN tenant_id UUID NOT NULL DEFAULT current_setting('app.tenant_id')::uuid;
ALTER TABLE llm_endpoint_secrets ADD COLUMN tenant_id UUID NOT NULL DEFAULT current_setting('app.tenant_id')::uuid;
ALTER TABLE llm_endpoints ADD COLUMN tenant_id UUID NOT NULL DEFAULT current_setting('app.tenant_id')::uuid;
ALTER TABLE llm_provider_configs ADD COLUMN tenant_id UUID NOT NULL DEFAULT current_setting('app.tenant_id')::uuid;
ALTER TABLE llm_provider_secrets ADD COLUMN tenant_id UUID NOT NULL DEFAULT current_setting('app.tenant_id')::uuid;
ALTER TABLE llm_usage ADD COLUMN tenant_id UUID NOT NULL DEFAULT current_setting('app.tenant_id')::uuid;
ALTER TABLE mcp_server_secrets ADD COLUMN tenant_id UUID NOT NULL DEFAULT current_setting('app.tenant_id')::uuid;
ALTER TABLE mcp_servers ADD COLUMN tenant_id UUID NOT NULL DEFAULT current_setting('app.tenant_id')::uuid;
ALTER TABLE mobile_devices ADD COLUMN tenant_id UUID NOT NULL DEFAULT current_setting('app.tenant_id')::uuid;
ALTER TABLE mobile_store_apps ADD COLUMN tenant_id UUID NOT NULL DEFAULT current_setting('app.tenant_id')::uuid;
ALTER TABLE model_prices ADD COLUMN tenant_id UUID NOT NULL DEFAULT current_setting('app.tenant_id')::uuid;
ALTER TABLE ops_audit_log ADD COLUMN tenant_id UUID NOT NULL DEFAULT current_setting('app.tenant_id')::uuid;
ALTER TABLE orchestration_plans ADD COLUMN tenant_id UUID NOT NULL DEFAULT current_setting('app.tenant_id')::uuid;
ALTER TABLE orchestrator_rules ADD COLUMN tenant_id UUID NOT NULL DEFAULT current_setting('app.tenant_id')::uuid;
ALTER TABLE plan_tasks ADD COLUMN tenant_id UUID NOT NULL DEFAULT current_setting('app.tenant_id')::uuid;
ALTER TABLE prod_incident_events ADD COLUMN tenant_id UUID NOT NULL DEFAULT current_setting('app.tenant_id')::uuid;
ALTER TABLE prod_incidents ADD COLUMN tenant_id UUID NOT NULL DEFAULT current_setting('app.tenant_id')::uuid;
ALTER TABLE projects ADD COLUMN tenant_id UUID NOT NULL DEFAULT current_setting('app.tenant_id')::uuid;
ALTER TABLE push_devices ADD COLUMN tenant_id UUID NOT NULL DEFAULT current_setting('app.tenant_id')::uuid;
ALTER TABLE quota_paused_tasks ADD COLUMN tenant_id UUID NOT NULL DEFAULT current_setting('app.tenant_id')::uuid;
ALTER TABLE repositories ADD COLUMN tenant_id UUID NOT NULL DEFAULT current_setting('app.tenant_id')::uuid;
ALTER TABLE repository_deploy_targets ADD COLUMN tenant_id UUID NOT NULL DEFAULT current_setting('app.tenant_id')::uuid;
ALTER TABLE repository_hosting_links ADD COLUMN tenant_id UUID NOT NULL DEFAULT current_setting('app.tenant_id')::uuid;
ALTER TABLE repository_pipeline_jobs ADD COLUMN tenant_id UUID NOT NULL DEFAULT current_setting('app.tenant_id')::uuid;
ALTER TABLE repository_profile_proposals ADD COLUMN tenant_id UUID NOT NULL DEFAULT current_setting('app.tenant_id')::uuid;
ALTER TABLE repository_profile_sections ADD COLUMN tenant_id UUID NOT NULL DEFAULT current_setting('app.tenant_id')::uuid;
ALTER TABLE repository_projects ADD COLUMN tenant_id UUID NOT NULL DEFAULT current_setting('app.tenant_id')::uuid;
ALTER TABLE session_actions ADD COLUMN tenant_id UUID NOT NULL DEFAULT current_setting('app.tenant_id')::uuid;
ALTER TABLE session_message_attachments ADD COLUMN tenant_id UUID NOT NULL DEFAULT current_setting('app.tenant_id')::uuid;
ALTER TABLE session_messages ADD COLUMN tenant_id UUID NOT NULL DEFAULT current_setting('app.tenant_id')::uuid;
ALTER TABLE session_runs ADD COLUMN tenant_id UUID NOT NULL DEFAULT current_setting('app.tenant_id')::uuid;
ALTER TABLE session_steps ADD COLUMN tenant_id UUID NOT NULL DEFAULT current_setting('app.tenant_id')::uuid;
ALTER TABLE sessions ADD COLUMN tenant_id UUID NOT NULL DEFAULT current_setting('app.tenant_id')::uuid;
ALTER TABLE signing_assets ADD COLUMN tenant_id UUID NOT NULL DEFAULT current_setting('app.tenant_id')::uuid;
ALTER TABLE skills ADD COLUMN tenant_id UUID NOT NULL DEFAULT current_setting('app.tenant_id')::uuid;
ALTER TABLE store_credentials ADD COLUMN tenant_id UUID NOT NULL DEFAULT current_setting('app.tenant_id')::uuid;
ALTER TABLE task_acceptance_criteria ADD COLUMN tenant_id UUID NOT NULL DEFAULT current_setting('app.tenant_id')::uuid;
ALTER TABLE task_agent_runs ADD COLUMN tenant_id UUID NOT NULL DEFAULT current_setting('app.tenant_id')::uuid;
ALTER TABLE task_attachments ADD COLUMN tenant_id UUID NOT NULL DEFAULT current_setting('app.tenant_id')::uuid;
ALTER TABLE task_column_spans ADD COLUMN tenant_id UUID NOT NULL DEFAULT current_setting('app.tenant_id')::uuid;
ALTER TABLE task_comments ADD COLUMN tenant_id UUID NOT NULL DEFAULT current_setting('app.tenant_id')::uuid;
ALTER TABLE task_criterion_checks ADD COLUMN tenant_id UUID NOT NULL DEFAULT current_setting('app.tenant_id')::uuid;
ALTER TABLE task_documents ADD COLUMN tenant_id UUID NOT NULL DEFAULT current_setting('app.tenant_id')::uuid;
ALTER TABLE task_pipeline_jobs ADD COLUMN tenant_id UUID NOT NULL DEFAULT current_setting('app.tenant_id')::uuid;
ALTER TABLE task_pipelines ADD COLUMN tenant_id UUID NOT NULL DEFAULT current_setting('app.tenant_id')::uuid;
ALTER TABLE task_relations ADD COLUMN tenant_id UUID NOT NULL DEFAULT current_setting('app.tenant_id')::uuid;
ALTER TABLE workspace_chunks ADD COLUMN tenant_id UUID NOT NULL DEFAULT current_setting('app.tenant_id')::uuid;
ALTER TABLE workspace_edges ADD COLUMN tenant_id UUID NOT NULL DEFAULT current_setting('app.tenant_id')::uuid;
ALTER TABLE workspace_file_hashes ADD COLUMN tenant_id UUID NOT NULL DEFAULT current_setting('app.tenant_id')::uuid;
ALTER TABLE workspace_indexes ADD COLUMN tenant_id UUID NOT NULL DEFAULT current_setting('app.tenant_id')::uuid;
ALTER TABLE workspace_symbols ADD COLUMN tenant_id UUID NOT NULL DEFAULT current_setting('app.tenant_id')::uuid;

-- ---------------------------------------------------------------------------
-- 2. row-level security
-- ---------------------------------------------------------------------------
--
-- One policy per table, identical everywhere, named identically everywhere, so
-- "does this table have isolation?" is one query against pg_policies rather
-- than a reading of 84 hand-written predicates. USING gates what a statement
-- can SEE; WITH CHECK gates what it can WRITE - both are needed, or a tenant
-- could not read another's rows but could still UPDATE one of its own into
-- somebody else's tenant_id.
--
-- What a policy still cannot do: PostgreSQL runs foreign-key checks with row
-- security off, so a FK pointing at another tenant's row is accepted by the
-- constraint even though the row is invisible. Every such FK here is a UUID
-- the other tenant never learns, and the only place worth closing structurally
-- is llm_provider_secrets -> llm_provider_configs below, whose key is a
-- provider NAME both tenants have.
-- ---------------------------------------------------------------------------
ALTER TABLE agent_cli_connection ENABLE ROW LEVEL SECURITY;
ALTER TABLE agent_cli_connection FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON agent_cli_connection
    USING (tenant_id = current_setting('app.tenant_id')::uuid)
    WITH CHECK (tenant_id = current_setting('app.tenant_id')::uuid);
ALTER TABLE agent_column_subscriptions ENABLE ROW LEVEL SECURITY;
ALTER TABLE agent_column_subscriptions FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON agent_column_subscriptions
    USING (tenant_id = current_setting('app.tenant_id')::uuid)
    WITH CHECK (tenant_id = current_setting('app.tenant_id')::uuid);
ALTER TABLE agent_evolution_events ENABLE ROW LEVEL SECURITY;
ALTER TABLE agent_evolution_events FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON agent_evolution_events
    USING (tenant_id = current_setting('app.tenant_id')::uuid)
    WITH CHECK (tenant_id = current_setting('app.tenant_id')::uuid);
ALTER TABLE agent_golden_results ENABLE ROW LEVEL SECURITY;
ALTER TABLE agent_golden_results FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON agent_golden_results
    USING (tenant_id = current_setting('app.tenant_id')::uuid)
    WITH CHECK (tenant_id = current_setting('app.tenant_id')::uuid);
ALTER TABLE agent_golden_tasks ENABLE ROW LEVEL SECURITY;
ALTER TABLE agent_golden_tasks FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON agent_golden_tasks
    USING (tenant_id = current_setting('app.tenant_id')::uuid)
    WITH CHECK (tenant_id = current_setting('app.tenant_id')::uuid);
ALTER TABLE agent_kpi_results ENABLE ROW LEVEL SECURITY;
ALTER TABLE agent_kpi_results FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON agent_kpi_results
    USING (tenant_id = current_setting('app.tenant_id')::uuid)
    WITH CHECK (tenant_id = current_setting('app.tenant_id')::uuid);
ALTER TABLE agent_kpis ENABLE ROW LEVEL SECURITY;
ALTER TABLE agent_kpis FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON agent_kpis
    USING (tenant_id = current_setting('app.tenant_id')::uuid)
    WITH CHECK (tenant_id = current_setting('app.tenant_id')::uuid);
ALTER TABLE agent_memories ENABLE ROW LEVEL SECURITY;
ALTER TABLE agent_memories FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON agent_memories
    USING (tenant_id = current_setting('app.tenant_id')::uuid)
    WITH CHECK (tenant_id = current_setting('app.tenant_id')::uuid);
ALTER TABLE agent_performance_scores ENABLE ROW LEVEL SECURITY;
ALTER TABLE agent_performance_scores FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON agent_performance_scores
    USING (tenant_id = current_setting('app.tenant_id')::uuid)
    WITH CHECK (tenant_id = current_setting('app.tenant_id')::uuid);
ALTER TABLE agent_reflections ENABLE ROW LEVEL SECURITY;
ALTER TABLE agent_reflections FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON agent_reflections
    USING (tenant_id = current_setting('app.tenant_id')::uuid)
    WITH CHECK (tenant_id = current_setting('app.tenant_id')::uuid);
ALTER TABLE agent_score_events ENABLE ROW LEVEL SECURITY;
ALTER TABLE agent_score_events FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON agent_score_events
    USING (tenant_id = current_setting('app.tenant_id')::uuid)
    WITH CHECK (tenant_id = current_setting('app.tenant_id')::uuid);
ALTER TABLE agent_templates ENABLE ROW LEVEL SECURITY;
ALTER TABLE agent_templates FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON agent_templates
    USING (tenant_id = current_setting('app.tenant_id')::uuid)
    WITH CHECK (tenant_id = current_setting('app.tenant_id')::uuid);
ALTER TABLE agents ENABLE ROW LEVEL SECURITY;
ALTER TABLE agents FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON agents
    USING (tenant_id = current_setting('app.tenant_id')::uuid)
    WITH CHECK (tenant_id = current_setting('app.tenant_id')::uuid);
ALTER TABLE api_keys ENABLE ROW LEVEL SECURITY;
ALTER TABLE api_keys FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON api_keys
    USING (tenant_id = current_setting('app.tenant_id')::uuid)
    WITH CHECK (tenant_id = current_setting('app.tenant_id')::uuid);
ALTER TABLE app_settings ENABLE ROW LEVEL SECURITY;
ALTER TABLE app_settings FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON app_settings
    USING (tenant_id = current_setting('app.tenant_id')::uuid)
    WITH CHECK (tenant_id = current_setting('app.tenant_id')::uuid);
ALTER TABLE attachments ENABLE ROW LEVEL SECURITY;
ALTER TABLE attachments FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON attachments
    USING (tenant_id = current_setting('app.tenant_id')::uuid)
    WITH CHECK (tenant_id = current_setting('app.tenant_id')::uuid);
ALTER TABLE audit_logs ENABLE ROW LEVEL SECURITY;
ALTER TABLE audit_logs FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON audit_logs
    USING (tenant_id = current_setting('app.tenant_id')::uuid)
    WITH CHECK (tenant_id = current_setting('app.tenant_id')::uuid);
ALTER TABLE billing_plan ENABLE ROW LEVEL SECURITY;
ALTER TABLE billing_plan FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON billing_plan
    USING (tenant_id = current_setting('app.tenant_id')::uuid)
    WITH CHECK (tenant_id = current_setting('app.tenant_id')::uuid);
ALTER TABLE board_column_transitions ENABLE ROW LEVEL SECURITY;
ALTER TABLE board_column_transitions FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON board_column_transitions
    USING (tenant_id = current_setting('app.tenant_id')::uuid)
    WITH CHECK (tenant_id = current_setting('app.tenant_id')::uuid);
ALTER TABLE board_columns ENABLE ROW LEVEL SECURITY;
ALTER TABLE board_columns FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON board_columns
    USING (tenant_id = current_setting('app.tenant_id')::uuid)
    WITH CHECK (tenant_id = current_setting('app.tenant_id')::uuid);
ALTER TABLE board_events ENABLE ROW LEVEL SECURITY;
ALTER TABLE board_events FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON board_events
    USING (tenant_id = current_setting('app.tenant_id')::uuid)
    WITH CHECK (tenant_id = current_setting('app.tenant_id')::uuid);
ALTER TABLE board_members ENABLE ROW LEVEL SECURITY;
ALTER TABLE board_members FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON board_members
    USING (tenant_id = current_setting('app.tenant_id')::uuid)
    WITH CHECK (tenant_id = current_setting('app.tenant_id')::uuid);
ALTER TABLE board_settings ENABLE ROW LEVEL SECURITY;
ALTER TABLE board_settings FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON board_settings
    USING (tenant_id = current_setting('app.tenant_id')::uuid)
    WITH CHECK (tenant_id = current_setting('app.tenant_id')::uuid);
ALTER TABLE board_task_counters ENABLE ROW LEVEL SECURITY;
ALTER TABLE board_task_counters FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON board_task_counters
    USING (tenant_id = current_setting('app.tenant_id')::uuid)
    WITH CHECK (tenant_id = current_setting('app.tenant_id')::uuid);
ALTER TABLE board_tasks ENABLE ROW LEVEL SECURITY;
ALTER TABLE board_tasks FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON board_tasks
    USING (tenant_id = current_setting('app.tenant_id')::uuid)
    WITH CHECK (tenant_id = current_setting('app.tenant_id')::uuid);
ALTER TABLE catalog_versions ENABLE ROW LEVEL SECURITY;
ALTER TABLE catalog_versions FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON catalog_versions
    USING (tenant_id = current_setting('app.tenant_id')::uuid)
    WITH CHECK (tenant_id = current_setting('app.tenant_id')::uuid);
ALTER TABLE deploy_dispatches ENABLE ROW LEVEL SECURITY;
ALTER TABLE deploy_dispatches FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON deploy_dispatches
    USING (tenant_id = current_setting('app.tenant_id')::uuid)
    WITH CHECK (tenant_id = current_setting('app.tenant_id')::uuid);
ALTER TABLE deploy_package_tasks ENABLE ROW LEVEL SECURITY;
ALTER TABLE deploy_package_tasks FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON deploy_package_tasks
    USING (tenant_id = current_setting('app.tenant_id')::uuid)
    WITH CHECK (tenant_id = current_setting('app.tenant_id')::uuid);
ALTER TABLE deploy_packages ENABLE ROW LEVEL SECURITY;
ALTER TABLE deploy_packages FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON deploy_packages
    USING (tenant_id = current_setting('app.tenant_id')::uuid)
    WITH CHECK (tenant_id = current_setting('app.tenant_id')::uuid);
ALTER TABLE deployment_runs ENABLE ROW LEVEL SECURITY;
ALTER TABLE deployment_runs FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON deployment_runs
    USING (tenant_id = current_setting('app.tenant_id')::uuid)
    WITH CHECK (tenant_id = current_setting('app.tenant_id')::uuid);
ALTER TABLE file_chunks ENABLE ROW LEVEL SECURITY;
ALTER TABLE file_chunks FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON file_chunks
    USING (tenant_id = current_setting('app.tenant_id')::uuid)
    WITH CHECK (tenant_id = current_setting('app.tenant_id')::uuid);
ALTER TABLE files ENABLE ROW LEVEL SECURITY;
ALTER TABLE files FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON files
    USING (tenant_id = current_setting('app.tenant_id')::uuid)
    WITH CHECK (tenant_id = current_setting('app.tenant_id')::uuid);
ALTER TABLE jobs ENABLE ROW LEVEL SECURITY;
ALTER TABLE jobs FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON jobs
    USING (tenant_id = current_setting('app.tenant_id')::uuid)
    WITH CHECK (tenant_id = current_setting('app.tenant_id')::uuid);
ALTER TABLE live_activity_tokens ENABLE ROW LEVEL SECURITY;
ALTER TABLE live_activity_tokens FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON live_activity_tokens
    USING (tenant_id = current_setting('app.tenant_id')::uuid)
    WITH CHECK (tenant_id = current_setting('app.tenant_id')::uuid);
ALTER TABLE llm_endpoint_secrets ENABLE ROW LEVEL SECURITY;
ALTER TABLE llm_endpoint_secrets FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON llm_endpoint_secrets
    USING (tenant_id = current_setting('app.tenant_id')::uuid)
    WITH CHECK (tenant_id = current_setting('app.tenant_id')::uuid);
ALTER TABLE llm_endpoints ENABLE ROW LEVEL SECURITY;
ALTER TABLE llm_endpoints FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON llm_endpoints
    USING (tenant_id = current_setting('app.tenant_id')::uuid)
    WITH CHECK (tenant_id = current_setting('app.tenant_id')::uuid);
ALTER TABLE llm_provider_configs ENABLE ROW LEVEL SECURITY;
ALTER TABLE llm_provider_configs FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON llm_provider_configs
    USING (tenant_id = current_setting('app.tenant_id')::uuid)
    WITH CHECK (tenant_id = current_setting('app.tenant_id')::uuid);
ALTER TABLE llm_provider_secrets ENABLE ROW LEVEL SECURITY;
ALTER TABLE llm_provider_secrets FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON llm_provider_secrets
    USING (tenant_id = current_setting('app.tenant_id')::uuid)
    WITH CHECK (tenant_id = current_setting('app.tenant_id')::uuid);
ALTER TABLE llm_usage ENABLE ROW LEVEL SECURITY;
ALTER TABLE llm_usage FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON llm_usage
    USING (tenant_id = current_setting('app.tenant_id')::uuid)
    WITH CHECK (tenant_id = current_setting('app.tenant_id')::uuid);
ALTER TABLE mcp_server_secrets ENABLE ROW LEVEL SECURITY;
ALTER TABLE mcp_server_secrets FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON mcp_server_secrets
    USING (tenant_id = current_setting('app.tenant_id')::uuid)
    WITH CHECK (tenant_id = current_setting('app.tenant_id')::uuid);
ALTER TABLE mcp_servers ENABLE ROW LEVEL SECURITY;
ALTER TABLE mcp_servers FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON mcp_servers
    USING (tenant_id = current_setting('app.tenant_id')::uuid)
    WITH CHECK (tenant_id = current_setting('app.tenant_id')::uuid);
ALTER TABLE mobile_devices ENABLE ROW LEVEL SECURITY;
ALTER TABLE mobile_devices FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON mobile_devices
    USING (tenant_id = current_setting('app.tenant_id')::uuid)
    WITH CHECK (tenant_id = current_setting('app.tenant_id')::uuid);
ALTER TABLE mobile_store_apps ENABLE ROW LEVEL SECURITY;
ALTER TABLE mobile_store_apps FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON mobile_store_apps
    USING (tenant_id = current_setting('app.tenant_id')::uuid)
    WITH CHECK (tenant_id = current_setting('app.tenant_id')::uuid);
ALTER TABLE model_prices ENABLE ROW LEVEL SECURITY;
ALTER TABLE model_prices FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON model_prices
    USING (tenant_id = current_setting('app.tenant_id')::uuid)
    WITH CHECK (tenant_id = current_setting('app.tenant_id')::uuid);
ALTER TABLE ops_audit_log ENABLE ROW LEVEL SECURITY;
ALTER TABLE ops_audit_log FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON ops_audit_log
    USING (tenant_id = current_setting('app.tenant_id')::uuid)
    WITH CHECK (tenant_id = current_setting('app.tenant_id')::uuid);
ALTER TABLE orchestration_plans ENABLE ROW LEVEL SECURITY;
ALTER TABLE orchestration_plans FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON orchestration_plans
    USING (tenant_id = current_setting('app.tenant_id')::uuid)
    WITH CHECK (tenant_id = current_setting('app.tenant_id')::uuid);
ALTER TABLE orchestrator_rules ENABLE ROW LEVEL SECURITY;
ALTER TABLE orchestrator_rules FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON orchestrator_rules
    USING (tenant_id = current_setting('app.tenant_id')::uuid)
    WITH CHECK (tenant_id = current_setting('app.tenant_id')::uuid);
ALTER TABLE plan_tasks ENABLE ROW LEVEL SECURITY;
ALTER TABLE plan_tasks FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON plan_tasks
    USING (tenant_id = current_setting('app.tenant_id')::uuid)
    WITH CHECK (tenant_id = current_setting('app.tenant_id')::uuid);
ALTER TABLE prod_incident_events ENABLE ROW LEVEL SECURITY;
ALTER TABLE prod_incident_events FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON prod_incident_events
    USING (tenant_id = current_setting('app.tenant_id')::uuid)
    WITH CHECK (tenant_id = current_setting('app.tenant_id')::uuid);
ALTER TABLE prod_incidents ENABLE ROW LEVEL SECURITY;
ALTER TABLE prod_incidents FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON prod_incidents
    USING (tenant_id = current_setting('app.tenant_id')::uuid)
    WITH CHECK (tenant_id = current_setting('app.tenant_id')::uuid);
ALTER TABLE projects ENABLE ROW LEVEL SECURITY;
ALTER TABLE projects FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON projects
    USING (tenant_id = current_setting('app.tenant_id')::uuid)
    WITH CHECK (tenant_id = current_setting('app.tenant_id')::uuid);
ALTER TABLE push_devices ENABLE ROW LEVEL SECURITY;
ALTER TABLE push_devices FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON push_devices
    USING (tenant_id = current_setting('app.tenant_id')::uuid)
    WITH CHECK (tenant_id = current_setting('app.tenant_id')::uuid);
ALTER TABLE quota_paused_tasks ENABLE ROW LEVEL SECURITY;
ALTER TABLE quota_paused_tasks FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON quota_paused_tasks
    USING (tenant_id = current_setting('app.tenant_id')::uuid)
    WITH CHECK (tenant_id = current_setting('app.tenant_id')::uuid);
ALTER TABLE repositories ENABLE ROW LEVEL SECURITY;
ALTER TABLE repositories FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON repositories
    USING (tenant_id = current_setting('app.tenant_id')::uuid)
    WITH CHECK (tenant_id = current_setting('app.tenant_id')::uuid);
ALTER TABLE repository_deploy_targets ENABLE ROW LEVEL SECURITY;
ALTER TABLE repository_deploy_targets FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON repository_deploy_targets
    USING (tenant_id = current_setting('app.tenant_id')::uuid)
    WITH CHECK (tenant_id = current_setting('app.tenant_id')::uuid);
ALTER TABLE repository_hosting_links ENABLE ROW LEVEL SECURITY;
ALTER TABLE repository_hosting_links FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON repository_hosting_links
    USING (tenant_id = current_setting('app.tenant_id')::uuid)
    WITH CHECK (tenant_id = current_setting('app.tenant_id')::uuid);
ALTER TABLE repository_pipeline_jobs ENABLE ROW LEVEL SECURITY;
ALTER TABLE repository_pipeline_jobs FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON repository_pipeline_jobs
    USING (tenant_id = current_setting('app.tenant_id')::uuid)
    WITH CHECK (tenant_id = current_setting('app.tenant_id')::uuid);
ALTER TABLE repository_profile_proposals ENABLE ROW LEVEL SECURITY;
ALTER TABLE repository_profile_proposals FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON repository_profile_proposals
    USING (tenant_id = current_setting('app.tenant_id')::uuid)
    WITH CHECK (tenant_id = current_setting('app.tenant_id')::uuid);
ALTER TABLE repository_profile_sections ENABLE ROW LEVEL SECURITY;
ALTER TABLE repository_profile_sections FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON repository_profile_sections
    USING (tenant_id = current_setting('app.tenant_id')::uuid)
    WITH CHECK (tenant_id = current_setting('app.tenant_id')::uuid);
ALTER TABLE repository_projects ENABLE ROW LEVEL SECURITY;
ALTER TABLE repository_projects FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON repository_projects
    USING (tenant_id = current_setting('app.tenant_id')::uuid)
    WITH CHECK (tenant_id = current_setting('app.tenant_id')::uuid);
ALTER TABLE session_actions ENABLE ROW LEVEL SECURITY;
ALTER TABLE session_actions FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON session_actions
    USING (tenant_id = current_setting('app.tenant_id')::uuid)
    WITH CHECK (tenant_id = current_setting('app.tenant_id')::uuid);
ALTER TABLE session_message_attachments ENABLE ROW LEVEL SECURITY;
ALTER TABLE session_message_attachments FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON session_message_attachments
    USING (tenant_id = current_setting('app.tenant_id')::uuid)
    WITH CHECK (tenant_id = current_setting('app.tenant_id')::uuid);
ALTER TABLE session_messages ENABLE ROW LEVEL SECURITY;
ALTER TABLE session_messages FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON session_messages
    USING (tenant_id = current_setting('app.tenant_id')::uuid)
    WITH CHECK (tenant_id = current_setting('app.tenant_id')::uuid);
ALTER TABLE session_runs ENABLE ROW LEVEL SECURITY;
ALTER TABLE session_runs FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON session_runs
    USING (tenant_id = current_setting('app.tenant_id')::uuid)
    WITH CHECK (tenant_id = current_setting('app.tenant_id')::uuid);
ALTER TABLE session_steps ENABLE ROW LEVEL SECURITY;
ALTER TABLE session_steps FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON session_steps
    USING (tenant_id = current_setting('app.tenant_id')::uuid)
    WITH CHECK (tenant_id = current_setting('app.tenant_id')::uuid);
ALTER TABLE sessions ENABLE ROW LEVEL SECURITY;
ALTER TABLE sessions FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON sessions
    USING (tenant_id = current_setting('app.tenant_id')::uuid)
    WITH CHECK (tenant_id = current_setting('app.tenant_id')::uuid);
ALTER TABLE signing_assets ENABLE ROW LEVEL SECURITY;
ALTER TABLE signing_assets FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON signing_assets
    USING (tenant_id = current_setting('app.tenant_id')::uuid)
    WITH CHECK (tenant_id = current_setting('app.tenant_id')::uuid);
ALTER TABLE skills ENABLE ROW LEVEL SECURITY;
ALTER TABLE skills FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON skills
    USING (tenant_id = current_setting('app.tenant_id')::uuid)
    WITH CHECK (tenant_id = current_setting('app.tenant_id')::uuid);
ALTER TABLE store_credentials ENABLE ROW LEVEL SECURITY;
ALTER TABLE store_credentials FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON store_credentials
    USING (tenant_id = current_setting('app.tenant_id')::uuid)
    WITH CHECK (tenant_id = current_setting('app.tenant_id')::uuid);
ALTER TABLE task_acceptance_criteria ENABLE ROW LEVEL SECURITY;
ALTER TABLE task_acceptance_criteria FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON task_acceptance_criteria
    USING (tenant_id = current_setting('app.tenant_id')::uuid)
    WITH CHECK (tenant_id = current_setting('app.tenant_id')::uuid);
ALTER TABLE task_agent_runs ENABLE ROW LEVEL SECURITY;
ALTER TABLE task_agent_runs FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON task_agent_runs
    USING (tenant_id = current_setting('app.tenant_id')::uuid)
    WITH CHECK (tenant_id = current_setting('app.tenant_id')::uuid);
ALTER TABLE task_attachments ENABLE ROW LEVEL SECURITY;
ALTER TABLE task_attachments FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON task_attachments
    USING (tenant_id = current_setting('app.tenant_id')::uuid)
    WITH CHECK (tenant_id = current_setting('app.tenant_id')::uuid);
ALTER TABLE task_column_spans ENABLE ROW LEVEL SECURITY;
ALTER TABLE task_column_spans FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON task_column_spans
    USING (tenant_id = current_setting('app.tenant_id')::uuid)
    WITH CHECK (tenant_id = current_setting('app.tenant_id')::uuid);
ALTER TABLE task_comments ENABLE ROW LEVEL SECURITY;
ALTER TABLE task_comments FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON task_comments
    USING (tenant_id = current_setting('app.tenant_id')::uuid)
    WITH CHECK (tenant_id = current_setting('app.tenant_id')::uuid);
ALTER TABLE task_criterion_checks ENABLE ROW LEVEL SECURITY;
ALTER TABLE task_criterion_checks FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON task_criterion_checks
    USING (tenant_id = current_setting('app.tenant_id')::uuid)
    WITH CHECK (tenant_id = current_setting('app.tenant_id')::uuid);
ALTER TABLE task_documents ENABLE ROW LEVEL SECURITY;
ALTER TABLE task_documents FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON task_documents
    USING (tenant_id = current_setting('app.tenant_id')::uuid)
    WITH CHECK (tenant_id = current_setting('app.tenant_id')::uuid);
ALTER TABLE task_pipeline_jobs ENABLE ROW LEVEL SECURITY;
ALTER TABLE task_pipeline_jobs FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON task_pipeline_jobs
    USING (tenant_id = current_setting('app.tenant_id')::uuid)
    WITH CHECK (tenant_id = current_setting('app.tenant_id')::uuid);
ALTER TABLE task_pipelines ENABLE ROW LEVEL SECURITY;
ALTER TABLE task_pipelines FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON task_pipelines
    USING (tenant_id = current_setting('app.tenant_id')::uuid)
    WITH CHECK (tenant_id = current_setting('app.tenant_id')::uuid);
ALTER TABLE task_relations ENABLE ROW LEVEL SECURITY;
ALTER TABLE task_relations FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON task_relations
    USING (tenant_id = current_setting('app.tenant_id')::uuid)
    WITH CHECK (tenant_id = current_setting('app.tenant_id')::uuid);
ALTER TABLE workspace_chunks ENABLE ROW LEVEL SECURITY;
ALTER TABLE workspace_chunks FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON workspace_chunks
    USING (tenant_id = current_setting('app.tenant_id')::uuid)
    WITH CHECK (tenant_id = current_setting('app.tenant_id')::uuid);
ALTER TABLE workspace_edges ENABLE ROW LEVEL SECURITY;
ALTER TABLE workspace_edges FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON workspace_edges
    USING (tenant_id = current_setting('app.tenant_id')::uuid)
    WITH CHECK (tenant_id = current_setting('app.tenant_id')::uuid);
ALTER TABLE workspace_file_hashes ENABLE ROW LEVEL SECURITY;
ALTER TABLE workspace_file_hashes FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON workspace_file_hashes
    USING (tenant_id = current_setting('app.tenant_id')::uuid)
    WITH CHECK (tenant_id = current_setting('app.tenant_id')::uuid);
ALTER TABLE workspace_indexes ENABLE ROW LEVEL SECURITY;
ALTER TABLE workspace_indexes FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON workspace_indexes
    USING (tenant_id = current_setting('app.tenant_id')::uuid)
    WITH CHECK (tenant_id = current_setting('app.tenant_id')::uuid);
ALTER TABLE workspace_symbols ENABLE ROW LEVEL SECURITY;
ALTER TABLE workspace_symbols FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON workspace_symbols
    USING (tenant_id = current_setting('app.tenant_id')::uuid)
    WITH CHECK (tenant_id = current_setting('app.tenant_id')::uuid);

-- ---------------------------------------------------------------------------
-- 3. UNIQUE constraints, re-cut to lead with tenant_id
-- ---------------------------------------------------------------------------
ALTER TABLE agent_golden_tasks DROP CONSTRAINT agent_golden_tasks_agent_id_name_key;
ALTER TABLE agent_golden_tasks ADD CONSTRAINT agent_golden_tasks_agent_id_name_key UNIQUE (tenant_id, agent_id, name);
ALTER TABLE agent_kpi_results DROP CONSTRAINT uq_agent_kpi_results_kpi_period;
ALTER TABLE agent_kpi_results ADD CONSTRAINT uq_agent_kpi_results_kpi_period UNIQUE (tenant_id, kpi_id, period_start);
ALTER TABLE agent_kpis DROP CONSTRAINT agent_kpis_agent_id_metric_key_period_key;
ALTER TABLE agent_kpis ADD CONSTRAINT agent_kpis_agent_id_metric_key_period_key UNIQUE (tenant_id, agent_id, metric_key, period);
ALTER TABLE agent_templates DROP CONSTRAINT agent_templates_name_key;
ALTER TABLE agent_templates ADD CONSTRAINT agent_templates_name_key UNIQUE (tenant_id, name);
ALTER TABLE agents DROP CONSTRAINT agents_name_key;
ALTER TABLE agents ADD CONSTRAINT agents_name_key UNIQUE (tenant_id, name);
ALTER TABLE api_keys DROP CONSTRAINT api_keys_key_hash_key;
ALTER TABLE api_keys ADD CONSTRAINT api_keys_key_hash_key UNIQUE (tenant_id, key_hash);
ALTER TABLE api_keys DROP CONSTRAINT api_keys_name_key;
ALTER TABLE api_keys ADD CONSTRAINT api_keys_name_key UNIQUE (tenant_id, name);
ALTER TABLE board_columns DROP CONSTRAINT board_columns_slug_key;
ALTER TABLE board_columns ADD CONSTRAINT board_columns_slug_key UNIQUE (tenant_id, slug);
ALTER TABLE catalog_versions DROP CONSTRAINT catalog_versions_target_kind_target_id_version_key;
ALTER TABLE catalog_versions ADD CONSTRAINT catalog_versions_target_kind_target_id_version_key UNIQUE (tenant_id, target_kind, target_id, version);
ALTER TABLE deployment_runs DROP CONSTRAINT deployment_runs_repository_id_run_id_key;
ALTER TABLE deployment_runs ADD CONSTRAINT deployment_runs_repository_id_run_id_key UNIQUE (tenant_id, repository_id, run_id);
ALTER TABLE live_activity_tokens DROP CONSTRAINT live_activity_tokens_token_key;
ALTER TABLE live_activity_tokens ADD CONSTRAINT live_activity_tokens_token_key UNIQUE (tenant_id, token);
ALTER TABLE llm_endpoints DROP CONSTRAINT llm_endpoints_name_key;
ALTER TABLE llm_endpoints ADD CONSTRAINT llm_endpoints_name_key UNIQUE (tenant_id, name);
ALTER TABLE mobile_store_apps DROP CONSTRAINT mobile_store_apps_repository_id_platform_key;
ALTER TABLE mobile_store_apps ADD CONSTRAINT mobile_store_apps_repository_id_platform_key UNIQUE (tenant_id, repository_id, platform);
ALTER TABLE plan_tasks DROP CONSTRAINT plan_tasks_plan_id_task_key_key;
ALTER TABLE plan_tasks ADD CONSTRAINT plan_tasks_plan_id_task_key_key UNIQUE (tenant_id, plan_id, task_key);
ALTER TABLE push_devices DROP CONSTRAINT push_devices_device_token_key;
ALTER TABLE push_devices ADD CONSTRAINT push_devices_device_token_key UNIQUE (tenant_id, device_token);
ALTER TABLE repositories DROP CONSTRAINT projects_root_path_key;
ALTER TABLE repositories ADD CONSTRAINT projects_root_path_key UNIQUE (tenant_id, root_path);
ALTER TABLE repository_deploy_targets DROP CONSTRAINT repository_deploy_targets_repository_id_env_key;
ALTER TABLE repository_deploy_targets ADD CONSTRAINT repository_deploy_targets_repository_id_env_key UNIQUE (tenant_id, repository_id, env);
ALTER TABLE repository_hosting_links DROP CONSTRAINT repository_hosting_links_repository_id_area_key;
ALTER TABLE repository_hosting_links ADD CONSTRAINT repository_hosting_links_repository_id_area_key UNIQUE (tenant_id, repository_id, area);
ALTER TABLE repository_pipeline_jobs DROP CONSTRAINT repository_pipeline_jobs_repository_id_sub_repo_kind_catego_key;
ALTER TABLE repository_pipeline_jobs ADD CONSTRAINT repository_pipeline_jobs_repository_id_sub_repo_kind_catego_key UNIQUE (tenant_id, repository_id, sub_repo_kind, category);
ALTER TABLE repository_profile_proposals DROP CONSTRAINT repository_profile_proposals_repository_id_field_slot_key;
ALTER TABLE repository_profile_proposals ADD CONSTRAINT repository_profile_proposals_repository_id_field_slot_key UNIQUE (tenant_id, repository_id, field, slot);
ALTER TABLE repository_profile_sections DROP CONSTRAINT repository_profile_sections_repository_id_section_key;
ALTER TABLE repository_profile_sections ADD CONSTRAINT repository_profile_sections_repository_id_section_key UNIQUE (tenant_id, repository_id, section);
ALTER TABLE signing_assets DROP CONSTRAINT signing_assets_kind_identifier_key;
ALTER TABLE signing_assets ADD CONSTRAINT signing_assets_kind_identifier_key UNIQUE (tenant_id, kind, identifier);
ALTER TABLE task_criterion_checks DROP CONSTRAINT task_criterion_checks_criterion_id_role_key;
ALTER TABLE task_criterion_checks ADD CONSTRAINT task_criterion_checks_criterion_id_role_key UNIQUE (tenant_id, criterion_id, role);
ALTER TABLE task_relations DROP CONSTRAINT task_relations_source_task_id_target_task_id_relation_type_key;
ALTER TABLE task_relations ADD CONSTRAINT task_relations_source_task_id_target_task_id_relation_type_key UNIQUE (tenant_id, source_task_id, target_task_id, relation_type);

-- ---------------------------------------------------------------------------
-- 4. natural-key PRIMARY KEYs
-- ---------------------------------------------------------------------------
--
-- A UUID `id` PK is already globally unique and is left alone - that is what
-- keeps the foreign keys pointed at one single-column and untouched. Only the
-- tables keyed on something a second tenant would legitimately repeat are
-- re-cut. **Do not read "the PK column is called id" as "it is a UUID
-- surrogate":** board_settings, billing_plan and mcp_servers all have a PK
-- column named `id` and none of the three is a surrogate. They are handled by
-- hand below.
--
-- Two of the re-cut keys have dependants, and both FKs are dropped, the key
-- widened, and the FK re-added on (tenant_id, ...):
--
--   llm_provider_configs  <- llm_provider_secrets(provider_type)
--   mcp_servers           <- mcp_server_secrets(server_id)
--
-- Those two are the only foreign keys in this schema that cannot be pointed at
-- another tenant's row. Every other FK here is single-column and RLS does NOT
-- cover it: referential-integrity checks run with row security disabled, so a
-- tenant that learns another tenant's uuid can make a child row referencing
-- it. That is accepted rather than overlooked - the parent keys are all
-- unguessable v4 uuids and the reference reveals nothing readable - but it is
-- the reason these two, whose parent keys are guessable NAMES, had to be
-- composite.
-- ---------------------------------------------------------------------------

ALTER TABLE llm_provider_secrets DROP CONSTRAINT llm_provider_secrets_provider_type_fkey;

-- PK agent_cli_connection (singleton)
ALTER TABLE agent_cli_connection DROP CONSTRAINT agent_cli_connection_pkey;
ALTER TABLE agent_cli_connection ADD CONSTRAINT agent_cli_connection_pkey PRIMARY KEY (tenant_id, singleton);
-- PK agent_column_subscriptions (agent_id, column_slug)
ALTER TABLE agent_column_subscriptions DROP CONSTRAINT agent_column_subscriptions_pkey;
ALTER TABLE agent_column_subscriptions ADD CONSTRAINT agent_column_subscriptions_pkey PRIMARY KEY (tenant_id, agent_id, column_slug);
-- PK app_settings (key)
ALTER TABLE app_settings DROP CONSTRAINT app_settings_pkey;
ALTER TABLE app_settings ADD CONSTRAINT app_settings_pkey PRIMARY KEY (tenant_id, key);
-- PK board_column_transitions (from_slug, to_slug)
ALTER TABLE board_column_transitions DROP CONSTRAINT board_column_transitions_pkey;
ALTER TABLE board_column_transitions ADD CONSTRAINT board_column_transitions_pkey PRIMARY KEY (tenant_id, from_slug, to_slug);
-- PK board_members (agent_id)
ALTER TABLE board_members DROP CONSTRAINT board_members_pkey;
ALTER TABLE board_members ADD CONSTRAINT board_members_pkey PRIMARY KEY (tenant_id, agent_id);
-- PK board_task_counters (task_type)
ALTER TABLE board_task_counters DROP CONSTRAINT board_task_counters_pkey;
ALTER TABLE board_task_counters ADD CONSTRAINT board_task_counters_pkey PRIMARY KEY (tenant_id, task_type);
-- PK deploy_package_tasks (package_id, task_id)
ALTER TABLE deploy_package_tasks DROP CONSTRAINT deploy_package_tasks_pkey;
ALTER TABLE deploy_package_tasks ADD CONSTRAINT deploy_package_tasks_pkey PRIMARY KEY (tenant_id, package_id, task_id);
-- PK llm_endpoint_secrets (endpoint_id)
ALTER TABLE llm_endpoint_secrets DROP CONSTRAINT llm_endpoint_secrets_pkey;
ALTER TABLE llm_endpoint_secrets ADD CONSTRAINT llm_endpoint_secrets_pkey PRIMARY KEY (tenant_id, endpoint_id);
-- PK llm_provider_configs (provider_type)
ALTER TABLE llm_provider_configs DROP CONSTRAINT llm_provider_configs_pkey;
ALTER TABLE llm_provider_configs ADD CONSTRAINT llm_provider_configs_pkey PRIMARY KEY (tenant_id, provider_type);
-- PK llm_provider_secrets (provider_type)
ALTER TABLE llm_provider_secrets DROP CONSTRAINT llm_provider_secrets_pkey;
ALTER TABLE llm_provider_secrets ADD CONSTRAINT llm_provider_secrets_pkey PRIMARY KEY (tenant_id, provider_type);
-- PK mcp_servers (id) and PK mcp_server_secrets (server_id, location, key).
--
-- mcp_servers is the trap this section warns about, sprung: its PK column is
-- literally named `id`, so the generator that produced this list skipped it
-- along with the UUID surrogates - but `mcp_servers.id` is TEXT and it is the
-- server's NAME, supplied by the caller ("github", "playwright", "context7";
-- see MCPStore.Create, which inserts domain.MCPServer.ID verbatim). Left
-- global it is broken in two directions at once:
--
--   * the second tenant to register a server called "github" gets 23505, and
--   * the FK below would let that tenant's secrets reference the FIRST
--     tenant's row - referential integrity checks run with row security OFF,
--     so RLS does not stop the reference - and then ON DELETE CASCADE from
--     tenant one's DELETE would take tenant two's secrets with it.
--
-- Both are fixed by the same widening. The FK is dropped and re-added on
-- (tenant_id, server_id) so that it, like llm_provider_secrets above, cannot
-- be pointed at another tenant's row.
ALTER TABLE mcp_server_secrets DROP CONSTRAINT mcp_server_secrets_server_id_fkey;
ALTER TABLE mcp_servers DROP CONSTRAINT mcp_servers_pkey;
ALTER TABLE mcp_servers ADD CONSTRAINT mcp_servers_pkey PRIMARY KEY (tenant_id, id);
ALTER TABLE mcp_server_secrets DROP CONSTRAINT mcp_server_secrets_pkey;
ALTER TABLE mcp_server_secrets ADD CONSTRAINT mcp_server_secrets_pkey PRIMARY KEY (tenant_id, server_id, location, key);
ALTER TABLE mcp_server_secrets
    ADD CONSTRAINT mcp_server_secrets_server_id_fkey
    FOREIGN KEY (tenant_id, server_id)
    REFERENCES mcp_servers(tenant_id, id) ON DELETE CASCADE;
-- PK model_prices (model)
ALTER TABLE model_prices DROP CONSTRAINT model_prices_pkey;
ALTER TABLE model_prices ADD CONSTRAINT model_prices_pkey PRIMARY KEY (tenant_id, model);
-- PK quota_paused_tasks (task_id)
ALTER TABLE quota_paused_tasks DROP CONSTRAINT quota_paused_tasks_pkey;
ALTER TABLE quota_paused_tasks ADD CONSTRAINT quota_paused_tasks_pkey PRIMARY KEY (tenant_id, task_id);
-- PK repository_projects (repository_id, project_id)
ALTER TABLE repository_projects DROP CONSTRAINT repository_projects_pkey;
ALTER TABLE repository_projects ADD CONSTRAINT repository_projects_pkey PRIMARY KEY (tenant_id, repository_id, project_id);
-- PK session_message_attachments (message_id, attachment_id)
ALTER TABLE session_message_attachments DROP CONSTRAINT session_message_attachments_pkey;
ALTER TABLE session_message_attachments ADD CONSTRAINT session_message_attachments_pkey PRIMARY KEY (tenant_id, message_id, attachment_id);
-- PK store_credentials (provider)
ALTER TABLE store_credentials DROP CONSTRAINT store_credentials_pkey;
ALTER TABLE store_credentials ADD CONSTRAINT store_credentials_pkey PRIMARY KEY (tenant_id, provider);
-- PK task_attachments (task_id, attachment_id)
ALTER TABLE task_attachments DROP CONSTRAINT task_attachments_pkey;
ALTER TABLE task_attachments ADD CONSTRAINT task_attachments_pkey PRIMARY KEY (tenant_id, task_id, attachment_id);
-- PK workspace_file_hashes (index_id, file_path)
ALTER TABLE workspace_file_hashes DROP CONSTRAINT workspace_file_hashes_pkey;
ALTER TABLE workspace_file_hashes ADD CONSTRAINT workspace_file_hashes_pkey PRIMARY KEY (tenant_id, index_id, file_path);

ALTER TABLE llm_provider_secrets
    ADD CONSTRAINT llm_provider_secrets_provider_type_fkey
    FOREIGN KEY (tenant_id, provider_type)
    REFERENCES llm_provider_configs(tenant_id, provider_type) ON DELETE CASCADE;

-- board_settings and billing_plan are the single-row tables: `id smallint
-- DEFAULT 1 CHECK (id = 1)` made "one row per install" a database fact. The
-- CHECK stays and the PK widens, so it now says one row per TENANT - the same
-- guarantee, one level down. The generator skipped these two because their PK
-- column is literally named `id`; they are not UUID surrogates, and they are
-- exactly the trap this migration exists to avoid, so they are handled by hand.
ALTER TABLE board_settings DROP CONSTRAINT board_settings_pkey;
ALTER TABLE board_settings ADD CONSTRAINT board_settings_pkey PRIMARY KEY (tenant_id, id);
ALTER TABLE billing_plan DROP CONSTRAINT billing_plan_pkey;
ALTER TABLE billing_plan ADD CONSTRAINT billing_plan_pkey PRIMARY KEY (tenant_id, id);

-- ---------------------------------------------------------------------------
-- 4b. every foreign key re-cut to carry tenant_id
-- ---------------------------------------------------------------------------
--
-- **Referential integrity checks run with row security OFF.** Postgres
-- enforces a foreign key with an internal query that is deliberately exempt
-- from RLS - it has to be, or a policy could make a parent row invisible and
-- silently break the constraint. The consequence for a shared database is
-- direct: a single-column `REFERENCES parent(id)` is enforced against EVERY
-- tenant's rows, not the referencing tenant's, so
--
--   * a tenant that learns another tenant's uuid can create a child row
--     pointing at it - a reference RLS never sees and never refuses, and
--   * the owner of that parent row then deletes it, and the ON DELETE action
--     travels down the constraint into the other tenant: CASCADE removes their
--     rows, SET NULL blanks their columns. Customer A's ordinary delete
--     destroys customer B's data, with no error on either side.
--
-- Measured, not theorised: before this section tenant B could insert a
-- board_tasks row referencing tenant A's repository, and A deleting that
-- repository took B's task with it.
--
-- The argument that the uuids are unguessable is true and not enough. Task and
-- repository ids appear in URLs, webhook payloads, agent transcripts, error
-- messages and support tickets; "hard to guess" is not a boundary, and the
-- failure it guards is silent cross-customer data loss.
--
-- So every one of the 82 cross-tenant foreign keys becomes composite:
--
--     FOREIGN KEY (tenant_id, <col>) REFERENCES parent(tenant_id, id)
--
-- which the FK's own exempt-from-RLS query enforces on the tenant_id pair, so a
-- reference into another tenant no longer satisfies the constraint at all. The
-- llm_provider_secrets and mcp_server_secrets keys re-cut in section 4 are the
-- same fix and are not repeated here.
--
-- MATCH SIMPLE, the default, is load-bearing: a composite FK with any NULL
-- column is not checked, and tenant_id is NOT NULL everywhere, so a nullable
-- child column (board_tasks.repository_id, session_runs.task_id …) keeps
-- behaving exactly as it did. MATCH FULL would reject those rows outright.
--
-- The 21 UNIQUE constraints below exist only because Postgres requires the
-- referenced columns to be a unique key of the parent. They sit ALONGSIDE the
-- surrogate `PRIMARY KEY (id)` rather than replacing it: dropping the single
-- column key would give up global uniqueness of `id` and force every lookup to
-- carry a tenant. One extra btree on 21 of 85 tables is the price.
--
-- Generated from the live catalog (pg_constraint), not by eye: every FK where
-- both ends are tenant-scoped and the child key did not already lead with
-- tenant_id, with confdeltype/confupdtype carried across verbatim.
-- ---------------------------------------------------------------------------

ALTER TABLE agent_evolution_events ADD CONSTRAINT agent_evolution_events_tenant_key UNIQUE (tenant_id, id);
ALTER TABLE agent_golden_tasks ADD CONSTRAINT agent_golden_tasks_tenant_key UNIQUE (tenant_id, id);
ALTER TABLE agent_kpis ADD CONSTRAINT agent_kpis_tenant_key UNIQUE (tenant_id, id);
ALTER TABLE agent_reflections ADD CONSTRAINT agent_reflections_tenant_key UNIQUE (tenant_id, id);
ALTER TABLE agents ADD CONSTRAINT agents_tenant_key UNIQUE (tenant_id, id);
ALTER TABLE attachments ADD CONSTRAINT attachments_tenant_key UNIQUE (tenant_id, id);
ALTER TABLE board_events ADD CONSTRAINT board_events_tenant_key UNIQUE (tenant_id, id);
ALTER TABLE board_tasks ADD CONSTRAINT board_tasks_tenant_key UNIQUE (tenant_id, id);
ALTER TABLE deploy_packages ADD CONSTRAINT deploy_packages_tenant_key UNIQUE (tenant_id, id);
ALTER TABLE files ADD CONSTRAINT files_tenant_key UNIQUE (tenant_id, id);
ALTER TABLE llm_endpoints ADD CONSTRAINT llm_endpoints_tenant_key UNIQUE (tenant_id, id);
ALTER TABLE orchestration_plans ADD CONSTRAINT orchestration_plans_tenant_key UNIQUE (tenant_id, id);
ALTER TABLE prod_incidents ADD CONSTRAINT prod_incidents_tenant_key UNIQUE (tenant_id, id);
ALTER TABLE projects ADD CONSTRAINT projects_tenant_key UNIQUE (tenant_id, id);
ALTER TABLE repositories ADD CONSTRAINT repositories_tenant_key UNIQUE (tenant_id, id);
ALTER TABLE session_messages ADD CONSTRAINT session_messages_tenant_key UNIQUE (tenant_id, id);
ALTER TABLE session_runs ADD CONSTRAINT session_runs_tenant_key UNIQUE (tenant_id, id);
ALTER TABLE sessions ADD CONSTRAINT sessions_tenant_key UNIQUE (tenant_id, id);
ALTER TABLE task_acceptance_criteria ADD CONSTRAINT task_acceptance_criteria_tenant_key UNIQUE (tenant_id, id);
ALTER TABLE task_pipelines ADD CONSTRAINT task_pipelines_tenant_key UNIQUE (tenant_id, id);
ALTER TABLE workspace_indexes ADD CONSTRAINT workspace_indexes_tenant_key UNIQUE (tenant_id, id);

ALTER TABLE agent_column_subscriptions DROP CONSTRAINT agent_column_subscriptions_agent_id_fkey;
ALTER TABLE agent_column_subscriptions ADD CONSTRAINT agent_column_subscriptions_agent_id_fkey FOREIGN KEY (tenant_id, agent_id) REFERENCES agents(tenant_id, id) ON DELETE CASCADE;
ALTER TABLE agent_evolution_events DROP CONSTRAINT agent_evolution_events_agent_id_fkey;
ALTER TABLE agent_evolution_events ADD CONSTRAINT agent_evolution_events_agent_id_fkey FOREIGN KEY (tenant_id, agent_id) REFERENCES agents(tenant_id, id) ON DELETE CASCADE;
ALTER TABLE agent_evolution_events DROP CONSTRAINT agent_evolution_events_reflection_id_fkey;
ALTER TABLE agent_evolution_events ADD CONSTRAINT agent_evolution_events_reflection_id_fkey FOREIGN KEY (tenant_id, reflection_id) REFERENCES agent_reflections(tenant_id, id) ON DELETE SET NULL;
ALTER TABLE agent_evolution_events DROP CONSTRAINT agent_evolution_events_reverted_event_id_fkey;
ALTER TABLE agent_evolution_events ADD CONSTRAINT agent_evolution_events_reverted_event_id_fkey FOREIGN KEY (tenant_id, reverted_event_id) REFERENCES agent_evolution_events(tenant_id, id) ON DELETE SET NULL;
ALTER TABLE agent_golden_results DROP CONSTRAINT agent_golden_results_agent_id_fkey;
ALTER TABLE agent_golden_results ADD CONSTRAINT agent_golden_results_agent_id_fkey FOREIGN KEY (tenant_id, agent_id) REFERENCES agents(tenant_id, id) ON DELETE CASCADE;
ALTER TABLE agent_golden_results DROP CONSTRAINT agent_golden_results_golden_id_fkey;
ALTER TABLE agent_golden_results ADD CONSTRAINT agent_golden_results_golden_id_fkey FOREIGN KEY (tenant_id, golden_id) REFERENCES agent_golden_tasks(tenant_id, id) ON DELETE CASCADE;
ALTER TABLE agent_golden_results DROP CONSTRAINT agent_golden_results_reflection_id_fkey;
ALTER TABLE agent_golden_results ADD CONSTRAINT agent_golden_results_reflection_id_fkey FOREIGN KEY (tenant_id, reflection_id) REFERENCES agent_reflections(tenant_id, id) ON DELETE SET NULL;
ALTER TABLE agent_golden_tasks DROP CONSTRAINT agent_golden_tasks_agent_id_fkey;
ALTER TABLE agent_golden_tasks ADD CONSTRAINT agent_golden_tasks_agent_id_fkey FOREIGN KEY (tenant_id, agent_id) REFERENCES agents(tenant_id, id) ON DELETE CASCADE;
ALTER TABLE agent_kpi_results DROP CONSTRAINT agent_kpi_results_agent_id_fkey;
ALTER TABLE agent_kpi_results ADD CONSTRAINT agent_kpi_results_agent_id_fkey FOREIGN KEY (tenant_id, agent_id) REFERENCES agents(tenant_id, id) ON DELETE CASCADE;
ALTER TABLE agent_kpi_results DROP CONSTRAINT agent_kpi_results_kpi_id_fkey;
ALTER TABLE agent_kpi_results ADD CONSTRAINT agent_kpi_results_kpi_id_fkey FOREIGN KEY (tenant_id, kpi_id) REFERENCES agent_kpis(tenant_id, id) ON DELETE CASCADE;
ALTER TABLE agent_kpis DROP CONSTRAINT agent_kpis_agent_id_fkey;
ALTER TABLE agent_kpis ADD CONSTRAINT agent_kpis_agent_id_fkey FOREIGN KEY (tenant_id, agent_id) REFERENCES agents(tenant_id, id) ON DELETE CASCADE;
ALTER TABLE agent_memories DROP CONSTRAINT agent_memories_agent_id_fkey;
ALTER TABLE agent_memories ADD CONSTRAINT agent_memories_agent_id_fkey FOREIGN KEY (tenant_id, agent_id) REFERENCES agents(tenant_id, id) ON DELETE CASCADE;
ALTER TABLE agent_memories DROP CONSTRAINT agent_memories_repository_id_fkey;
ALTER TABLE agent_memories ADD CONSTRAINT agent_memories_repository_id_fkey FOREIGN KEY (tenant_id, repository_id) REFERENCES repositories(tenant_id, id) ON DELETE CASCADE;
ALTER TABLE agent_performance_scores DROP CONSTRAINT agent_performance_scores_agent_id_fkey;
ALTER TABLE agent_performance_scores ADD CONSTRAINT agent_performance_scores_agent_id_fkey FOREIGN KEY (tenant_id, agent_id) REFERENCES agents(tenant_id, id) ON DELETE CASCADE;
ALTER TABLE agent_reflections DROP CONSTRAINT agent_reflections_agent_id_fkey;
ALTER TABLE agent_reflections ADD CONSTRAINT agent_reflections_agent_id_fkey FOREIGN KEY (tenant_id, agent_id) REFERENCES agents(tenant_id, id) ON DELETE CASCADE;
ALTER TABLE agent_score_events DROP CONSTRAINT agent_score_events_agent_id_fkey;
ALTER TABLE agent_score_events ADD CONSTRAINT agent_score_events_agent_id_fkey FOREIGN KEY (tenant_id, agent_id) REFERENCES agents(tenant_id, id) ON DELETE CASCADE;
ALTER TABLE agent_score_events DROP CONSTRAINT agent_score_events_task_id_fkey;
ALTER TABLE agent_score_events ADD CONSTRAINT agent_score_events_task_id_fkey FOREIGN KEY (tenant_id, task_id) REFERENCES board_tasks(tenant_id, id) ON DELETE SET NULL;
ALTER TABLE attachments DROP CONSTRAINT attachments_repository_id_fkey;
ALTER TABLE attachments ADD CONSTRAINT attachments_repository_id_fkey FOREIGN KEY (tenant_id, repository_id) REFERENCES repositories(tenant_id, id) ON DELETE CASCADE;
ALTER TABLE board_events DROP CONSTRAINT board_events_project_id_fkey;
ALTER TABLE board_events ADD CONSTRAINT board_events_project_id_fkey FOREIGN KEY (tenant_id, repository_id) REFERENCES repositories(tenant_id, id) ON DELETE CASCADE;
ALTER TABLE board_events DROP CONSTRAINT board_events_task_id_fkey;
ALTER TABLE board_events ADD CONSTRAINT board_events_task_id_fkey FOREIGN KEY (tenant_id, task_id) REFERENCES board_tasks(tenant_id, id) ON DELETE CASCADE;
ALTER TABLE board_members DROP CONSTRAINT board_members_agent_id_fkey;
ALTER TABLE board_members ADD CONSTRAINT board_members_agent_id_fkey FOREIGN KEY (tenant_id, agent_id) REFERENCES agents(tenant_id, id) ON DELETE CASCADE;
ALTER TABLE board_tasks DROP CONSTRAINT board_tasks_initiative_project_id_fkey;
ALTER TABLE board_tasks ADD CONSTRAINT board_tasks_initiative_project_id_fkey FOREIGN KEY (tenant_id, initiative_project_id) REFERENCES projects(tenant_id, id) ON DELETE SET NULL;
ALTER TABLE board_tasks DROP CONSTRAINT project_tasks_assignee_agent_id_fkey;
ALTER TABLE board_tasks ADD CONSTRAINT project_tasks_assignee_agent_id_fkey FOREIGN KEY (tenant_id, assignee_agent_id) REFERENCES agents(tenant_id, id) ON DELETE SET NULL;
ALTER TABLE board_tasks DROP CONSTRAINT project_tasks_project_id_fkey;
ALTER TABLE board_tasks ADD CONSTRAINT project_tasks_project_id_fkey FOREIGN KEY (tenant_id, repository_id) REFERENCES repositories(tenant_id, id) ON DELETE CASCADE;
ALTER TABLE deploy_dispatches DROP CONSTRAINT deploy_dispatches_repository_id_fkey;
ALTER TABLE deploy_dispatches ADD CONSTRAINT deploy_dispatches_repository_id_fkey FOREIGN KEY (tenant_id, repository_id) REFERENCES repositories(tenant_id, id) ON DELETE CASCADE;
ALTER TABLE deploy_package_tasks DROP CONSTRAINT deploy_package_tasks_package_id_fkey;
ALTER TABLE deploy_package_tasks ADD CONSTRAINT deploy_package_tasks_package_id_fkey FOREIGN KEY (tenant_id, package_id) REFERENCES deploy_packages(tenant_id, id) ON DELETE CASCADE;
ALTER TABLE deploy_package_tasks DROP CONSTRAINT deploy_package_tasks_task_id_fkey;
ALTER TABLE deploy_package_tasks ADD CONSTRAINT deploy_package_tasks_task_id_fkey FOREIGN KEY (tenant_id, task_id) REFERENCES board_tasks(tenant_id, id) ON DELETE CASCADE;
ALTER TABLE deploy_packages DROP CONSTRAINT deploy_packages_repository_id_fkey;
ALTER TABLE deploy_packages ADD CONSTRAINT deploy_packages_repository_id_fkey FOREIGN KEY (tenant_id, repository_id) REFERENCES repositories(tenant_id, id) ON DELETE CASCADE;
ALTER TABLE deployment_runs DROP CONSTRAINT deployment_runs_repository_id_fkey;
ALTER TABLE deployment_runs ADD CONSTRAINT deployment_runs_repository_id_fkey FOREIGN KEY (tenant_id, repository_id) REFERENCES repositories(tenant_id, id) ON DELETE CASCADE;
ALTER TABLE file_chunks DROP CONSTRAINT file_chunks_file_id_fkey;
ALTER TABLE file_chunks ADD CONSTRAINT file_chunks_file_id_fkey FOREIGN KEY (tenant_id, file_id) REFERENCES files(tenant_id, id) ON DELETE CASCADE;
ALTER TABLE llm_endpoint_secrets DROP CONSTRAINT llm_endpoint_secrets_endpoint_id_fkey;
ALTER TABLE llm_endpoint_secrets ADD CONSTRAINT llm_endpoint_secrets_endpoint_id_fkey FOREIGN KEY (tenant_id, endpoint_id) REFERENCES llm_endpoints(tenant_id, id) ON DELETE CASCADE;
ALTER TABLE mobile_store_apps DROP CONSTRAINT mobile_store_apps_repository_id_fkey;
ALTER TABLE mobile_store_apps ADD CONSTRAINT mobile_store_apps_repository_id_fkey FOREIGN KEY (tenant_id, repository_id) REFERENCES repositories(tenant_id, id) ON DELETE CASCADE;
ALTER TABLE ops_audit_log DROP CONSTRAINT ops_audit_log_repository_id_fkey;
ALTER TABLE ops_audit_log ADD CONSTRAINT ops_audit_log_repository_id_fkey FOREIGN KEY (tenant_id, repository_id) REFERENCES repositories(tenant_id, id) ON DELETE SET NULL;
ALTER TABLE orchestration_plans DROP CONSTRAINT orchestration_plans_run_id_fkey;
ALTER TABLE orchestration_plans ADD CONSTRAINT orchestration_plans_run_id_fkey FOREIGN KEY (tenant_id, run_id) REFERENCES session_runs(tenant_id, id) ON DELETE CASCADE;
ALTER TABLE orchestrator_rules DROP CONSTRAINT orchestrator_rules_agent_id_fkey;
ALTER TABLE orchestrator_rules ADD CONSTRAINT orchestrator_rules_agent_id_fkey FOREIGN KEY (tenant_id, agent_id) REFERENCES agents(tenant_id, id) ON DELETE CASCADE;
ALTER TABLE plan_tasks DROP CONSTRAINT plan_tasks_agent_id_fkey;
ALTER TABLE plan_tasks ADD CONSTRAINT plan_tasks_agent_id_fkey FOREIGN KEY (tenant_id, agent_id) REFERENCES agents(tenant_id, id) ON DELETE SET NULL;
ALTER TABLE plan_tasks DROP CONSTRAINT plan_tasks_plan_id_fkey;
ALTER TABLE plan_tasks ADD CONSTRAINT plan_tasks_plan_id_fkey FOREIGN KEY (tenant_id, plan_id) REFERENCES orchestration_plans(tenant_id, id) ON DELETE CASCADE;
ALTER TABLE prod_incident_events DROP CONSTRAINT prod_incident_events_incident_id_fkey;
ALTER TABLE prod_incident_events ADD CONSTRAINT prod_incident_events_incident_id_fkey FOREIGN KEY (tenant_id, incident_id) REFERENCES prod_incidents(tenant_id, id) ON DELETE CASCADE;
ALTER TABLE prod_incidents DROP CONSTRAINT prod_incidents_repository_id_fkey;
ALTER TABLE prod_incidents ADD CONSTRAINT prod_incidents_repository_id_fkey FOREIGN KEY (tenant_id, repository_id) REFERENCES repositories(tenant_id, id) ON DELETE CASCADE;
ALTER TABLE prod_incidents DROP CONSTRAINT prod_incidents_task_id_fkey;
ALTER TABLE prod_incidents ADD CONSTRAINT prod_incidents_task_id_fkey FOREIGN KEY (tenant_id, task_id) REFERENCES board_tasks(tenant_id, id) ON DELETE SET NULL;
ALTER TABLE repository_deploy_targets DROP CONSTRAINT repository_deploy_targets_repository_id_fkey;
ALTER TABLE repository_deploy_targets ADD CONSTRAINT repository_deploy_targets_repository_id_fkey FOREIGN KEY (tenant_id, repository_id) REFERENCES repositories(tenant_id, id) ON DELETE CASCADE;
ALTER TABLE repository_hosting_links DROP CONSTRAINT repository_hosting_links_repository_id_fkey;
ALTER TABLE repository_hosting_links ADD CONSTRAINT repository_hosting_links_repository_id_fkey FOREIGN KEY (tenant_id, repository_id) REFERENCES repositories(tenant_id, id) ON DELETE CASCADE;
ALTER TABLE repository_pipeline_jobs DROP CONSTRAINT repository_pipeline_jobs_repository_id_fkey;
ALTER TABLE repository_pipeline_jobs ADD CONSTRAINT repository_pipeline_jobs_repository_id_fkey FOREIGN KEY (tenant_id, repository_id) REFERENCES repositories(tenant_id, id) ON DELETE CASCADE;
ALTER TABLE repository_profile_proposals DROP CONSTRAINT repository_profile_proposals_repository_id_fkey;
ALTER TABLE repository_profile_proposals ADD CONSTRAINT repository_profile_proposals_repository_id_fkey FOREIGN KEY (tenant_id, repository_id) REFERENCES repositories(tenant_id, id) ON DELETE CASCADE;
ALTER TABLE repository_profile_sections DROP CONSTRAINT repository_profile_sections_repository_id_fkey;
ALTER TABLE repository_profile_sections ADD CONSTRAINT repository_profile_sections_repository_id_fkey FOREIGN KEY (tenant_id, repository_id) REFERENCES repositories(tenant_id, id) ON DELETE CASCADE;
ALTER TABLE repository_projects DROP CONSTRAINT repository_projects_project_id_fkey;
ALTER TABLE repository_projects ADD CONSTRAINT repository_projects_project_id_fkey FOREIGN KEY (tenant_id, project_id) REFERENCES projects(tenant_id, id) ON DELETE CASCADE;
ALTER TABLE repository_projects DROP CONSTRAINT repository_projects_repository_id_fkey;
ALTER TABLE repository_projects ADD CONSTRAINT repository_projects_repository_id_fkey FOREIGN KEY (tenant_id, repository_id) REFERENCES repositories(tenant_id, id) ON DELETE CASCADE;
ALTER TABLE session_actions DROP CONSTRAINT session_actions_session_id_fkey;
ALTER TABLE session_actions ADD CONSTRAINT session_actions_session_id_fkey FOREIGN KEY (tenant_id, session_id) REFERENCES sessions(tenant_id, id) ON DELETE CASCADE;
ALTER TABLE session_message_attachments DROP CONSTRAINT session_message_attachments_attachment_id_fkey;
ALTER TABLE session_message_attachments ADD CONSTRAINT session_message_attachments_attachment_id_fkey FOREIGN KEY (tenant_id, attachment_id) REFERENCES attachments(tenant_id, id) ON DELETE CASCADE;
ALTER TABLE session_message_attachments DROP CONSTRAINT session_message_attachments_message_id_fkey;
ALTER TABLE session_message_attachments ADD CONSTRAINT session_message_attachments_message_id_fkey FOREIGN KEY (tenant_id, message_id) REFERENCES session_messages(tenant_id, id) ON DELETE CASCADE;
ALTER TABLE session_messages DROP CONSTRAINT session_messages_session_id_fkey;
ALTER TABLE session_messages ADD CONSTRAINT session_messages_session_id_fkey FOREIGN KEY (tenant_id, session_id) REFERENCES sessions(tenant_id, id) ON DELETE CASCADE;
ALTER TABLE session_runs DROP CONSTRAINT session_runs_session_id_fkey;
ALTER TABLE session_runs ADD CONSTRAINT session_runs_session_id_fkey FOREIGN KEY (tenant_id, session_id) REFERENCES sessions(tenant_id, id) ON DELETE CASCADE;
ALTER TABLE session_steps DROP CONSTRAINT session_steps_run_id_fkey;
ALTER TABLE session_steps ADD CONSTRAINT session_steps_run_id_fkey FOREIGN KEY (tenant_id, run_id) REFERENCES session_runs(tenant_id, id) ON DELETE CASCADE;
ALTER TABLE sessions DROP CONSTRAINT sessions_agent_id_fkey;
ALTER TABLE sessions ADD CONSTRAINT sessions_agent_id_fkey FOREIGN KEY (tenant_id, agent_id) REFERENCES agents(tenant_id, id) ON DELETE CASCADE;
ALTER TABLE sessions DROP CONSTRAINT sessions_project_id_fkey;
ALTER TABLE sessions ADD CONSTRAINT sessions_project_id_fkey FOREIGN KEY (tenant_id, repository_id) REFERENCES repositories(tenant_id, id) ON DELETE CASCADE;
ALTER TABLE sessions DROP CONSTRAINT sessions_task_id_fkey;
ALTER TABLE sessions ADD CONSTRAINT sessions_task_id_fkey FOREIGN KEY (tenant_id, task_id) REFERENCES board_tasks(tenant_id, id) ON DELETE SET NULL;
ALTER TABLE skills DROP CONSTRAINT skills_agent_id_fkey;
ALTER TABLE skills ADD CONSTRAINT skills_agent_id_fkey FOREIGN KEY (tenant_id, agent_id) REFERENCES agents(tenant_id, id) ON DELETE CASCADE;
ALTER TABLE task_acceptance_criteria DROP CONSTRAINT task_acceptance_criteria_task_id_fkey;
ALTER TABLE task_acceptance_criteria ADD CONSTRAINT task_acceptance_criteria_task_id_fkey FOREIGN KEY (tenant_id, task_id) REFERENCES board_tasks(tenant_id, id) ON DELETE CASCADE;
ALTER TABLE task_agent_runs DROP CONSTRAINT task_agent_runs_agent_id_fkey;
ALTER TABLE task_agent_runs ADD CONSTRAINT task_agent_runs_agent_id_fkey FOREIGN KEY (tenant_id, agent_id) REFERENCES agents(tenant_id, id) ON DELETE CASCADE;
ALTER TABLE task_agent_runs DROP CONSTRAINT task_agent_runs_board_event_id_fkey;
ALTER TABLE task_agent_runs ADD CONSTRAINT task_agent_runs_board_event_id_fkey FOREIGN KEY (tenant_id, board_event_id) REFERENCES board_events(tenant_id, id) ON DELETE CASCADE;
ALTER TABLE task_agent_runs DROP CONSTRAINT task_agent_runs_session_run_id_fkey;
ALTER TABLE task_agent_runs ADD CONSTRAINT task_agent_runs_session_run_id_fkey FOREIGN KEY (tenant_id, session_run_id) REFERENCES session_runs(tenant_id, id) ON DELETE SET NULL;
ALTER TABLE task_agent_runs DROP CONSTRAINT task_agent_runs_task_id_fkey;
ALTER TABLE task_agent_runs ADD CONSTRAINT task_agent_runs_task_id_fkey FOREIGN KEY (tenant_id, task_id) REFERENCES board_tasks(tenant_id, id) ON DELETE CASCADE;
ALTER TABLE task_attachments DROP CONSTRAINT task_attachments_attachment_id_fkey;
ALTER TABLE task_attachments ADD CONSTRAINT task_attachments_attachment_id_fkey FOREIGN KEY (tenant_id, attachment_id) REFERENCES attachments(tenant_id, id) ON DELETE CASCADE;
ALTER TABLE task_attachments DROP CONSTRAINT task_attachments_task_id_fkey;
ALTER TABLE task_attachments ADD CONSTRAINT task_attachments_task_id_fkey FOREIGN KEY (tenant_id, task_id) REFERENCES board_tasks(tenant_id, id) ON DELETE CASCADE;
ALTER TABLE task_column_spans DROP CONSTRAINT task_column_spans_agent_id_fkey;
ALTER TABLE task_column_spans ADD CONSTRAINT task_column_spans_agent_id_fkey FOREIGN KEY (tenant_id, agent_id) REFERENCES agents(tenant_id, id) ON DELETE SET NULL;
ALTER TABLE task_column_spans DROP CONSTRAINT task_column_spans_repository_id_fkey;
ALTER TABLE task_column_spans ADD CONSTRAINT task_column_spans_repository_id_fkey FOREIGN KEY (tenant_id, repository_id) REFERENCES repositories(tenant_id, id) ON DELETE CASCADE;
ALTER TABLE task_column_spans DROP CONSTRAINT task_column_spans_task_id_fkey;
ALTER TABLE task_column_spans ADD CONSTRAINT task_column_spans_task_id_fkey FOREIGN KEY (tenant_id, task_id) REFERENCES board_tasks(tenant_id, id) ON DELETE CASCADE;
ALTER TABLE task_comments DROP CONSTRAINT project_task_comments_task_id_fkey;
ALTER TABLE task_comments ADD CONSTRAINT project_task_comments_task_id_fkey FOREIGN KEY (tenant_id, task_id) REFERENCES board_tasks(tenant_id, id) ON DELETE CASCADE;
ALTER TABLE task_criterion_checks DROP CONSTRAINT task_criterion_checks_agent_id_fkey;
ALTER TABLE task_criterion_checks ADD CONSTRAINT task_criterion_checks_agent_id_fkey FOREIGN KEY (tenant_id, agent_id) REFERENCES agents(tenant_id, id) ON DELETE SET NULL;
ALTER TABLE task_criterion_checks DROP CONSTRAINT task_criterion_checks_criterion_id_fkey;
ALTER TABLE task_criterion_checks ADD CONSTRAINT task_criterion_checks_criterion_id_fkey FOREIGN KEY (tenant_id, criterion_id) REFERENCES task_acceptance_criteria(tenant_id, id) ON DELETE CASCADE;
ALTER TABLE task_documents DROP CONSTRAINT task_documents_task_id_fkey;
ALTER TABLE task_documents ADD CONSTRAINT task_documents_task_id_fkey FOREIGN KEY (tenant_id, task_id) REFERENCES board_tasks(tenant_id, id) ON DELETE CASCADE;
ALTER TABLE task_pipeline_jobs DROP CONSTRAINT task_pipeline_jobs_pipeline_id_fkey;
ALTER TABLE task_pipeline_jobs ADD CONSTRAINT task_pipeline_jobs_pipeline_id_fkey FOREIGN KEY (tenant_id, pipeline_id) REFERENCES task_pipelines(tenant_id, id) ON DELETE CASCADE;
ALTER TABLE task_pipelines DROP CONSTRAINT task_pipelines_repository_id_fkey;
ALTER TABLE task_pipelines ADD CONSTRAINT task_pipelines_repository_id_fkey FOREIGN KEY (tenant_id, repository_id) REFERENCES repositories(tenant_id, id) ON DELETE CASCADE;
ALTER TABLE task_pipelines DROP CONSTRAINT task_pipelines_task_id_fkey;
ALTER TABLE task_pipelines ADD CONSTRAINT task_pipelines_task_id_fkey FOREIGN KEY (tenant_id, task_id) REFERENCES board_tasks(tenant_id, id) ON DELETE CASCADE;
ALTER TABLE task_relations DROP CONSTRAINT task_relations_source_task_id_fkey;
ALTER TABLE task_relations ADD CONSTRAINT task_relations_source_task_id_fkey FOREIGN KEY (tenant_id, source_task_id) REFERENCES board_tasks(tenant_id, id) ON DELETE CASCADE;
ALTER TABLE task_relations DROP CONSTRAINT task_relations_target_task_id_fkey;
ALTER TABLE task_relations ADD CONSTRAINT task_relations_target_task_id_fkey FOREIGN KEY (tenant_id, target_task_id) REFERENCES board_tasks(tenant_id, id) ON DELETE CASCADE;
ALTER TABLE workspace_chunks DROP CONSTRAINT workspace_chunks_index_id_fkey;
ALTER TABLE workspace_chunks ADD CONSTRAINT workspace_chunks_index_id_fkey FOREIGN KEY (tenant_id, index_id) REFERENCES workspace_indexes(tenant_id, id) ON DELETE CASCADE;
ALTER TABLE workspace_edges DROP CONSTRAINT workspace_edges_index_id_fkey;
ALTER TABLE workspace_edges ADD CONSTRAINT workspace_edges_index_id_fkey FOREIGN KEY (tenant_id, index_id) REFERENCES workspace_indexes(tenant_id, id) ON DELETE CASCADE;
ALTER TABLE workspace_file_hashes DROP CONSTRAINT workspace_file_hashes_index_id_fkey;
ALTER TABLE workspace_file_hashes ADD CONSTRAINT workspace_file_hashes_index_id_fkey FOREIGN KEY (tenant_id, index_id) REFERENCES workspace_indexes(tenant_id, id) ON DELETE CASCADE;
ALTER TABLE workspace_indexes DROP CONSTRAINT workspace_indexes_project_id_fkey;
ALTER TABLE workspace_indexes ADD CONSTRAINT workspace_indexes_project_id_fkey FOREIGN KEY (tenant_id, repository_id) REFERENCES repositories(tenant_id, id) ON DELETE CASCADE;
ALTER TABLE workspace_indexes DROP CONSTRAINT workspace_indexes_session_id_fkey;
ALTER TABLE workspace_indexes ADD CONSTRAINT workspace_indexes_session_id_fkey FOREIGN KEY (tenant_id, session_id) REFERENCES sessions(tenant_id, id) ON DELETE CASCADE;
ALTER TABLE workspace_symbols DROP CONSTRAINT workspace_symbols_index_id_fkey;
ALTER TABLE workspace_symbols ADD CONSTRAINT workspace_symbols_index_id_fkey FOREIGN KEY (tenant_id, index_id) REFERENCES workspace_indexes(tenant_id, id) ON DELETE CASCADE;

-- ---------------------------------------------------------------------------
-- 5. UNIQUE indexes (the ones that are indexes rather than constraints)
-- ---------------------------------------------------------------------------
--
-- idx_board_tasks_type_task_number is the sharpest of these: it is what makes
-- task keys (T-12, B-3, A-7) unique, and without tenant_id in front the second
-- tenant's first task would collide with the first tenant's first task.
-- ---------------------------------------------------------------------------
DROP INDEX uq_agent_scores_agent;
CREATE UNIQUE INDEX uq_agent_scores_agent ON agent_performance_scores (tenant_id, agent_id);
DROP INDEX idx_board_tasks_type_task_number;
CREATE UNIQUE INDEX idx_board_tasks_type_task_number ON board_tasks (tenant_id, task_type, task_number);
DROP INDEX idx_mobile_devices_udid;
CREATE UNIQUE INDEX idx_mobile_devices_udid ON mobile_devices (tenant_id, device_udid) WHERE (device_udid <> ''::text);
DROP INDEX idx_rules_agent_name;
CREATE UNIQUE INDEX idx_rules_agent_name ON orchestrator_rules (tenant_id, agent_id, name);
DROP INDEX idx_prod_incidents_live_fingerprint;
CREATE UNIQUE INDEX idx_prod_incidents_live_fingerprint ON prod_incidents (tenant_id, repository_id, env, fingerprint) WHERE (status <> ALL (ARRAY['resolved'::text, 'ignored'::text]));
DROP INDEX idx_skills_agent_name;
CREATE UNIQUE INDEX idx_skills_agent_name ON skills (tenant_id, agent_id, name);
DROP INDEX idx_task_agent_runs_one_pending;
CREATE UNIQUE INDEX idx_task_agent_runs_one_pending ON task_agent_runs (tenant_id, task_id, agent_id) WHERE (status = 'pending'::text);
DROP INDEX idx_workspace_indexes_repo_branch;
CREATE UNIQUE INDEX idx_workspace_indexes_repo_branch ON workspace_indexes (tenant_id, repository_id, branch) WHERE (repository_id IS NOT NULL);
DROP INDEX idx_workspace_indexes_repository;
CREATE UNIQUE INDEX idx_workspace_indexes_repository ON workspace_indexes (tenant_id, repository_id) WHERE (repository_id IS NOT NULL);
DROP INDEX idx_workspace_indexes_session;
CREATE UNIQUE INDEX idx_workspace_indexes_session ON workspace_indexes (tenant_id, session_id);

-- ---------------------------------------------------------------------------
-- 6. lookup indexes re-led with tenant_id
-- ---------------------------------------------------------------------------
--
-- Only the indexes whose leading column is a value MANY tenants share: a
-- status, a flag, a timestamp, a model name. Those are the sweeper and
-- dashboard queries, and unprefixed each one would walk the whole fleet's rows
-- to find one tenant's.
--
-- Deliberately NOT re-led: every index already leading with a tenant-scoped
-- UUID (task_id, agent_id, repository_id, index_id, session_id, plan_id,
-- incident_id, file_id, package_id ...). Those already narrow to one tenant's
-- rows in one descent; a tenant_id prefix would only make each entry 16 bytes
-- wider. idx_workspace_chunks_content_trgm is left alone for a different
-- reason - it is a GIN trigram index with no leading btree column to prepend
-- to; the RLS predicate filters after its bitmap scan.
-- ---------------------------------------------------------------------------
DROP INDEX idx_agent_evolution_events_pending;
CREATE INDEX idx_agent_evolution_events_pending ON agent_evolution_events (tenant_id, impact, created_at) WHERE (impact = 'pending'::text);
DROP INDEX idx_agents_enabled;
CREATE INDEX idx_agents_enabled ON agents (tenant_id, enabled);
DROP INDEX idx_audit_logs_created_at;
CREATE INDEX idx_audit_logs_created_at ON audit_logs (tenant_id, created_at DESC);
DROP INDEX idx_audit_logs_request_id;
CREATE INDEX idx_audit_logs_request_id ON audit_logs (tenant_id, request_id);
DROP INDEX idx_board_events_created;
CREATE INDEX idx_board_events_created ON board_events (tenant_id, created_at DESC);
DROP INDEX idx_board_tasks_blocked_resource;
CREATE INDEX idx_board_tasks_blocked_resource ON board_tasks (tenant_id, blocked_resource, blocked_at) WHERE (blocked_resource IS NOT NULL);
DROP INDEX idx_board_tasks_merge_commit;
CREATE INDEX idx_board_tasks_merge_commit ON board_tasks (tenant_id, merge_commit_sha) WHERE (merge_commit_sha IS NOT NULL);
DROP INDEX idx_catalog_versions_target;
CREATE INDEX idx_catalog_versions_target ON catalog_versions (tenant_id, target_kind, target_id, version DESC);
DROP INDEX idx_jobs_created_at;
CREATE INDEX idx_jobs_created_at ON jobs (tenant_id, created_at DESC);
DROP INDEX idx_jobs_status;
CREATE INDEX idx_jobs_status ON jobs (tenant_id, status);
DROP INDEX idx_llm_usage_created;
CREATE INDEX idx_llm_usage_created ON llm_usage (tenant_id, created_at);
DROP INDEX idx_llm_usage_model;
CREATE INDEX idx_llm_usage_model ON llm_usage (tenant_id, model, created_at);
DROP INDEX idx_orchestration_plans_status;
CREATE INDEX idx_orchestration_plans_status ON orchestration_plans (tenant_id, status);
DROP INDEX idx_orchestrator_rules_priority;
CREATE INDEX idx_orchestrator_rules_priority ON orchestrator_rules (tenant_id, priority DESC);
DROP INDEX idx_plan_tasks_status;
CREATE INDEX idx_plan_tasks_status ON plan_tasks (tenant_id, status);
DROP INDEX idx_repositories_updated_at;
CREATE INDEX idx_repositories_updated_at ON repositories (tenant_id, updated_at DESC);
DROP INDEX idx_session_runs_status;
CREATE INDEX idx_session_runs_status ON session_runs (tenant_id, status);
DROP INDEX idx_sessions_expires_at;
CREATE INDEX idx_sessions_expires_at ON sessions (tenant_id, expires_at);
DROP INDEX idx_skills_category;
CREATE INDEX idx_skills_category ON skills (tenant_id, category);
DROP INDEX idx_skills_enabled;
CREATE INDEX idx_skills_enabled ON skills (tenant_id, enabled);
DROP INDEX idx_task_agent_runs_active;
CREATE INDEX idx_task_agent_runs_active ON task_agent_runs (tenant_id, status, updated_at) WHERE (status = ANY (ARRAY['pending'::text, 'running'::text]));
DROP INDEX idx_task_agent_runs_quota_resume;
CREATE INDEX idx_task_agent_runs_quota_resume ON task_agent_runs (tenant_id, quota_resume_at) WHERE (quota_resume_at IS NOT NULL);
DROP INDEX idx_task_pipelines_active;
CREATE INDEX idx_task_pipelines_active ON task_pipelines (tenant_id, status, created_at) WHERE (status = ANY (ARRAY['pending'::text, 'running'::text]));
DROP INDEX idx_task_pipelines_unfinished;
CREATE INDEX idx_task_pipelines_unfinished ON task_pipelines (tenant_id, created_at) WHERE (status = ANY (ARRAY['pending'::text, 'running'::text]));

-- ---------------------------------------------------------------------------
-- 7. the install-wide seed rows
-- ---------------------------------------------------------------------------
--
-- Migrations 010/018/041/042/049/051/054/056/093 and friends INSERTed the
-- board columns, the task counters, the provider list, the model prices and
-- the single settings rows - correct when a database WAS a tenant, and
-- meaningless now: those rows have just been stamped with the placeholder
-- tenant above and belong to nobody. Per-tenant seeding moved to
-- `internal/application/tenantboot`, which runs on first sight of a tenant.
--
-- Clearing them here rather than leaving them is deliberate: rows nobody can
-- ever read are indistinguishable from a bug, and a future reader finding 13
-- board columns owned by the nil UUID would reasonably assume a backfill was
-- forgotten. These statements go through the policies just written, against
-- the placeholder tenant set at the top - so they are also the first proof in
-- this file that the policies let a correctly scoped statement through.
DELETE FROM board_column_transitions;
DELETE FROM board_columns;
DELETE FROM board_task_counters;
DELETE FROM board_settings;
DELETE FROM billing_plan;
DELETE FROM app_settings;
DELETE FROM model_prices;
DELETE FROM llm_provider_secrets;
DELETE FROM llm_provider_configs;
