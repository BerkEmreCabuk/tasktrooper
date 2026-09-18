-- One-shot, idempotent data migration: brings existing installs' role-agent
-- KPIs onto the recalibrated defaultRoleKPIs set
-- (internal/application/catalog/seed.go) as of this change — the old targets
-- were far too lenient and several new metrics (first_pass_rate,
-- gate_rejected_runs) never reached agents seeded before they existed.
--
-- Matches an existing row by (agent name, KPI name, case-insensitive/trimmed)
-- so an admin's own differently-named KPIs are never touched. A match is
-- updated in place (metric_key/period/targets/weight/enabled only — an
-- admin's custom description is left alone) so its id, and any results tied
-- to it, survive; no match gets a new row. Safe to run more than once: the
-- UPDATE is a no-op the second time and the INSERT's NOT EXISTS / ON CONFLICT
-- guard stop it from duplicating.
WITH role_kpis (agent_name, kpi_name, metric_key, period, target_full, target_half, weight, enabled) AS (
    VALUES
    -- system-architect
    ('system-architect', 'Weekly tasks completed', 'tasks_completed', 'weekly', 10, 4, 1.0, true),
    ('system-architect', 'Weekly revisions', 'revisions_received', 'weekly', 1, 3, 1.0, true),
    ('system-architect', 'First-pass rate', 'first_pass_rate', 'weekly', 90, 75, 2.0, true),
    ('system-architect', 'Clean review time', 'clean_time_code_review', 'weekly', 0.5, 2, 1.0, true),
    ('system-architect', 'Clean analysis time', 'clean_time_in_progress', 'weekly', 2, 6, 1.0, true),
    ('system-architect', 'Weekly review escapes', 'review_escapes', 'weekly', 0, 1, 2.0, true),
    ('system-architect', 'Gate-rejected runs', 'gate_rejected_runs', 'weekly', 0, 2, 1.5, true),
    ('system-architect', 'Tool error rate', 'tool_error_rate', 'weekly', 3, 6, 0.5, true),
    -- backend-developer / frontend-developer / mobile-developer share one set
    ('backend-developer', 'Weekly tasks completed', 'tasks_completed', 'weekly', 10, 4, 1.0, true),
    ('backend-developer', 'Weekly revisions', 'revisions_received', 'weekly', 1, 3, 1.0, true),
    ('backend-developer', 'First-pass rate', 'first_pass_rate', 'weekly', 90, 75, 2.0, true),
    ('backend-developer', 'Weekly UAT rejections', 'uat_failures', 'weekly', 0, 2, 1.5, true),
    ('backend-developer', 'Weekly bugs', 'bugs_assigned', 'weekly', 2, 5, 1.0, true),
    ('backend-developer', 'Clean cycle time (in progress)', 'clean_time_in_progress', 'weekly', 1.5, 4, 1.0, true),
    ('backend-developer', 'Tool error rate', 'tool_error_rate', 'weekly', 3, 6, 0.5, true),
    ('frontend-developer', 'Weekly tasks completed', 'tasks_completed', 'weekly', 10, 4, 1.0, true),
    ('frontend-developer', 'Weekly revisions', 'revisions_received', 'weekly', 1, 3, 1.0, true),
    ('frontend-developer', 'First-pass rate', 'first_pass_rate', 'weekly', 90, 75, 2.0, true),
    ('frontend-developer', 'Weekly UAT rejections', 'uat_failures', 'weekly', 0, 2, 1.5, true),
    ('frontend-developer', 'Weekly bugs', 'bugs_assigned', 'weekly', 2, 5, 1.0, true),
    ('frontend-developer', 'Clean cycle time (in progress)', 'clean_time_in_progress', 'weekly', 1.5, 4, 1.0, true),
    ('frontend-developer', 'Tool error rate', 'tool_error_rate', 'weekly', 3, 6, 0.5, true),
    ('mobile-developer', 'Weekly tasks completed', 'tasks_completed', 'weekly', 10, 4, 1.0, true),
    ('mobile-developer', 'Weekly revisions', 'revisions_received', 'weekly', 1, 3, 1.0, true),
    ('mobile-developer', 'First-pass rate', 'first_pass_rate', 'weekly', 90, 75, 2.0, true),
    ('mobile-developer', 'Weekly UAT rejections', 'uat_failures', 'weekly', 0, 2, 1.5, true),
    ('mobile-developer', 'Weekly bugs', 'bugs_assigned', 'weekly', 2, 5, 1.0, true),
    ('mobile-developer', 'Clean cycle time (in progress)', 'clean_time_in_progress', 'weekly', 1.5, 4, 1.0, true),
    ('mobile-developer', 'Tool error rate', 'tool_error_rate', 'weekly', 3, 6, 0.5, true),
    -- qa-agent
    ('qa-agent', 'Weekly tasks completed', 'tasks_completed', 'weekly', 5, 2, 1.0, true),
    ('qa-agent', 'Weekly UAT escapes', 'uat_failures', 'weekly', 0, 2, 1.5, true),
    ('qa-agent', 'Clean QA time', 'clean_time_in_qa', 'weekly', 0.5, 2, 1.0, true),
    ('qa-agent', 'Gate-rejected runs', 'gate_rejected_runs', 'weekly', 0, 2, 1.5, true),
    ('qa-agent', 'Tool error rate', 'tool_error_rate', 'weekly', 3, 6, 0.5, true),
    -- product-manager
    ('product-manager', 'Weekly tasks completed', 'tasks_completed', 'weekly', 5, 2, 1.0, true),
    ('product-manager', 'Clean UAT time', 'clean_time_pm_uat', 'weekly', 0.25, 1, 1.0, true),
    ('product-manager', 'Gate-rejected runs', 'gate_rejected_runs', 'weekly', 0, 2, 1.5, true),
    ('product-manager', 'Tool error rate', 'tool_error_rate', 'weekly', 3, 6, 0.5, true)
),
updated AS (
    UPDATE agent_kpis k
    SET metric_key  = r.metric_key,
        period      = r.period,
        target_full = r.target_full,
        target_half = r.target_half,
        weight      = r.weight,
        enabled     = r.enabled,
        updated_at  = now()
    FROM role_kpis r
    JOIN agents a ON a.name = r.agent_name
    WHERE k.agent_id = a.id
      AND lower(trim(k.name)) = lower(trim(r.kpi_name))
    RETURNING a.id AS agent_id, lower(trim(r.kpi_name)) AS kpi_key
)
INSERT INTO agent_kpis (agent_id, metric_key, name, description, period, target_full, target_half, weight, enabled)
SELECT a.id, r.metric_key, r.kpi_name, '', r.period, r.target_full, r.target_half, r.weight, r.enabled
FROM role_kpis r
JOIN agents a ON a.name = r.agent_name
LEFT JOIN updated u ON u.agent_id = a.id AND u.kpi_key = lower(trim(r.kpi_name))
WHERE u.agent_id IS NULL
ON CONFLICT (agent_id, metric_key, period) DO NOTHING;
