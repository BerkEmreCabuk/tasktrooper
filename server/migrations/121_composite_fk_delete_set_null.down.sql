-- See the matching comment in the .up.sql: ADD CONSTRAINT revalidates
-- existing rows, these tables carry FORCE ROW LEVEL SECURITY, and a
-- migration has no tenant in it to satisfy current_setting('app.tenant_id').
-- Local to this transaction (one tx.Exec per file); the nil UUID hides
-- every row from the scan, which is correct since the JOIN condition is
-- unchanged either direction of this migration.
SELECT set_config('app.tenant_id', '00000000-0000-0000-0000-000000000000', true);

ALTER TABLE agent_evolution_events DROP CONSTRAINT agent_evolution_events_reflection_id_fkey;
ALTER TABLE agent_evolution_events ADD CONSTRAINT agent_evolution_events_reflection_id_fkey
    FOREIGN KEY (tenant_id, reflection_id) REFERENCES agent_reflections(tenant_id, id) ON DELETE SET NULL;

ALTER TABLE agent_evolution_events DROP CONSTRAINT agent_evolution_events_reverted_event_id_fkey;
ALTER TABLE agent_evolution_events ADD CONSTRAINT agent_evolution_events_reverted_event_id_fkey
    FOREIGN KEY (tenant_id, reverted_event_id) REFERENCES agent_evolution_events(tenant_id, id) ON DELETE SET NULL;

ALTER TABLE agent_golden_results DROP CONSTRAINT agent_golden_results_reflection_id_fkey;
ALTER TABLE agent_golden_results ADD CONSTRAINT agent_golden_results_reflection_id_fkey
    FOREIGN KEY (tenant_id, reflection_id) REFERENCES agent_reflections(tenant_id, id) ON DELETE SET NULL;

ALTER TABLE agent_score_events DROP CONSTRAINT agent_score_events_task_id_fkey;
ALTER TABLE agent_score_events ADD CONSTRAINT agent_score_events_task_id_fkey
    FOREIGN KEY (tenant_id, task_id) REFERENCES board_tasks(tenant_id, id) ON DELETE SET NULL;

ALTER TABLE board_tasks DROP CONSTRAINT board_tasks_initiative_project_id_fkey;
ALTER TABLE board_tasks ADD CONSTRAINT board_tasks_initiative_project_id_fkey
    FOREIGN KEY (tenant_id, initiative_project_id) REFERENCES projects(tenant_id, id) ON DELETE SET NULL;

ALTER TABLE board_tasks DROP CONSTRAINT project_tasks_assignee_agent_id_fkey;
ALTER TABLE board_tasks ADD CONSTRAINT project_tasks_assignee_agent_id_fkey
    FOREIGN KEY (tenant_id, assignee_agent_id) REFERENCES agents(tenant_id, id) ON DELETE SET NULL;

ALTER TABLE ops_audit_log DROP CONSTRAINT ops_audit_log_repository_id_fkey;
ALTER TABLE ops_audit_log ADD CONSTRAINT ops_audit_log_repository_id_fkey
    FOREIGN KEY (tenant_id, repository_id) REFERENCES repositories(tenant_id, id) ON DELETE SET NULL;

ALTER TABLE plan_tasks DROP CONSTRAINT plan_tasks_agent_id_fkey;
ALTER TABLE plan_tasks ADD CONSTRAINT plan_tasks_agent_id_fkey
    FOREIGN KEY (tenant_id, agent_id) REFERENCES agents(tenant_id, id) ON DELETE SET NULL;

ALTER TABLE prod_incidents DROP CONSTRAINT prod_incidents_task_id_fkey;
ALTER TABLE prod_incidents ADD CONSTRAINT prod_incidents_task_id_fkey
    FOREIGN KEY (tenant_id, task_id) REFERENCES board_tasks(tenant_id, id) ON DELETE SET NULL;

ALTER TABLE sessions DROP CONSTRAINT sessions_task_id_fkey;
ALTER TABLE sessions ADD CONSTRAINT sessions_task_id_fkey
    FOREIGN KEY (tenant_id, task_id) REFERENCES board_tasks(tenant_id, id) ON DELETE SET NULL;

ALTER TABLE task_agent_runs DROP CONSTRAINT task_agent_runs_session_run_id_fkey;
ALTER TABLE task_agent_runs ADD CONSTRAINT task_agent_runs_session_run_id_fkey
    FOREIGN KEY (tenant_id, session_run_id) REFERENCES session_runs(tenant_id, id) ON DELETE SET NULL;

ALTER TABLE task_column_spans DROP CONSTRAINT task_column_spans_agent_id_fkey;
ALTER TABLE task_column_spans ADD CONSTRAINT task_column_spans_agent_id_fkey
    FOREIGN KEY (tenant_id, agent_id) REFERENCES agents(tenant_id, id) ON DELETE SET NULL;

ALTER TABLE task_criterion_checks DROP CONSTRAINT task_criterion_checks_agent_id_fkey;
ALTER TABLE task_criterion_checks ADD CONSTRAINT task_criterion_checks_agent_id_fkey
    FOREIGN KEY (tenant_id, agent_id) REFERENCES agents(tenant_id, id) ON DELETE SET NULL;
