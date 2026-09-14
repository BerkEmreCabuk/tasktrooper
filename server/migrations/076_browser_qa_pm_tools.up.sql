-- Browser tools for QA and PM. Tool policies are written on agent CREATE only
-- (admin customizations must survive restarts), so existing installs need the
-- new tools backfilled into the QA and PM policies — same pattern as migration
-- 073's review_criterion backfill: one guarded UPDATE per tool so re-running
-- never appends a duplicate.
--
-- qa-agent additionally gets get_pipeline_status and get_deploy_target here:
-- both are in the code-side policy already but were never backfilled to
-- existing installs (known gap), so this migration closes it.
--
-- product-manager additionally gets the read-only code tools (to verify real
-- file/endpoint names before filling technical_description) and
-- get_deploy_target (to resolve the stage base_url for pm_uat browsing).

-- Browser tools: both roles.
UPDATE agents
SET tool_policy = jsonb_set(
        tool_policy,
        '{allow_tools}',
        COALESCE(tool_policy->'allow_tools', '[]'::jsonb) || '["browser_navigate"]'::jsonb
    )
WHERE name IN ('qa-agent', 'product-manager')
  AND NOT COALESCE(tool_policy->'allow_tools', '[]'::jsonb) ? 'browser_navigate';

UPDATE agents
SET tool_policy = jsonb_set(
        tool_policy,
        '{allow_tools}',
        COALESCE(tool_policy->'allow_tools', '[]'::jsonb) || '["browser_screenshot"]'::jsonb
    )
WHERE name IN ('qa-agent', 'product-manager')
  AND NOT COALESCE(tool_policy->'allow_tools', '[]'::jsonb) ? 'browser_screenshot';

UPDATE agents
SET tool_policy = jsonb_set(
        tool_policy,
        '{allow_tools}',
        COALESCE(tool_policy->'allow_tools', '[]'::jsonb) || '["browser_click"]'::jsonb
    )
WHERE name IN ('qa-agent', 'product-manager')
  AND NOT COALESCE(tool_policy->'allow_tools', '[]'::jsonb) ? 'browser_click';

UPDATE agents
SET tool_policy = jsonb_set(
        tool_policy,
        '{allow_tools}',
        COALESCE(tool_policy->'allow_tools', '[]'::jsonb) || '["browser_fill"]'::jsonb
    )
WHERE name IN ('qa-agent', 'product-manager')
  AND NOT COALESCE(tool_policy->'allow_tools', '[]'::jsonb) ? 'browser_fill';

UPDATE agents
SET tool_policy = jsonb_set(
        tool_policy,
        '{allow_tools}',
        COALESCE(tool_policy->'allow_tools', '[]'::jsonb) || '["browser_read_dom"]'::jsonb
    )
WHERE name IN ('qa-agent', 'product-manager')
  AND NOT COALESCE(tool_policy->'allow_tools', '[]'::jsonb) ? 'browser_read_dom';

UPDATE agents
SET tool_policy = jsonb_set(
        tool_policy,
        '{allow_tools}',
        COALESCE(tool_policy->'allow_tools', '[]'::jsonb) || '["browser_wait_for"]'::jsonb
    )
WHERE name IN ('qa-agent', 'product-manager')
  AND NOT COALESCE(tool_policy->'allow_tools', '[]'::jsonb) ? 'browser_wait_for';

-- qa-agent: pipeline/deploy tools never backfilled before.
UPDATE agents
SET tool_policy = jsonb_set(
        tool_policy,
        '{allow_tools}',
        COALESCE(tool_policy->'allow_tools', '[]'::jsonb) || '["get_pipeline_status"]'::jsonb
    )
WHERE name = 'qa-agent'
  AND NOT COALESCE(tool_policy->'allow_tools', '[]'::jsonb) ? 'get_pipeline_status';

UPDATE agents
SET tool_policy = jsonb_set(
        tool_policy,
        '{allow_tools}',
        COALESCE(tool_policy->'allow_tools', '[]'::jsonb) || '["get_deploy_target"]'::jsonb
    )
WHERE name = 'qa-agent'
  AND NOT COALESCE(tool_policy->'allow_tools', '[]'::jsonb) ? 'get_deploy_target';

-- product-manager: read-only code tools + stage deploy target.
UPDATE agents
SET tool_policy = jsonb_set(
        tool_policy,
        '{allow_tools}',
        COALESCE(tool_policy->'allow_tools', '[]'::jsonb) || '["codebase_search"]'::jsonb
    )
WHERE name = 'product-manager'
  AND NOT COALESCE(tool_policy->'allow_tools', '[]'::jsonb) ? 'codebase_search';

UPDATE agents
SET tool_policy = jsonb_set(
        tool_policy,
        '{allow_tools}',
        COALESCE(tool_policy->'allow_tools', '[]'::jsonb) || '["grep_code"]'::jsonb
    )
WHERE name = 'product-manager'
  AND NOT COALESCE(tool_policy->'allow_tools', '[]'::jsonb) ? 'grep_code';

UPDATE agents
SET tool_policy = jsonb_set(
        tool_policy,
        '{allow_tools}',
        COALESCE(tool_policy->'allow_tools', '[]'::jsonb) || '["get_repo_tree"]'::jsonb
    )
WHERE name = 'product-manager'
  AND NOT COALESCE(tool_policy->'allow_tools', '[]'::jsonb) ? 'get_repo_tree';

UPDATE agents
SET tool_policy = jsonb_set(
        tool_policy,
        '{allow_tools}',
        COALESCE(tool_policy->'allow_tools', '[]'::jsonb) || '["get_symbol_skeleton"]'::jsonb
    )
WHERE name = 'product-manager'
  AND NOT COALESCE(tool_policy->'allow_tools', '[]'::jsonb) ? 'get_symbol_skeleton';

UPDATE agents
SET tool_policy = jsonb_set(
        tool_policy,
        '{allow_tools}',
        COALESCE(tool_policy->'allow_tools', '[]'::jsonb) || '["expand_symbol_context"]'::jsonb
    )
WHERE name = 'product-manager'
  AND NOT COALESCE(tool_policy->'allow_tools', '[]'::jsonb) ? 'expand_symbol_context';

UPDATE agents
SET tool_policy = jsonb_set(
        tool_policy,
        '{allow_tools}',
        COALESCE(tool_policy->'allow_tools', '[]'::jsonb) || '["get_deploy_target"]'::jsonb
    )
WHERE name = 'product-manager'
  AND NOT COALESCE(tool_policy->'allow_tools', '[]'::jsonb) ? 'get_deploy_target';
