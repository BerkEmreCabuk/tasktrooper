-- Removals are scoped by agent name: other roles legitimately carry some of
-- these tools (architect has get_pipeline_status, developers have the code
-- tools), so a blanket removal like 073's would strip policies this migration
-- never touched.

-- Browser tools: both roles.
UPDATE agents
SET tool_policy = jsonb_set(
        tool_policy,
        '{allow_tools}',
        (tool_policy->'allow_tools') - 'browser_navigate' - 'browser_screenshot' - 'browser_click' - 'browser_fill' - 'browser_read_dom' - 'browser_wait_for'
    )
WHERE name IN ('qa-agent', 'product-manager')
  AND tool_policy->'allow_tools' ?| ARRAY['browser_navigate', 'browser_screenshot', 'browser_click', 'browser_fill', 'browser_read_dom', 'browser_wait_for'];

-- qa-agent: pipeline/deploy backfill.
UPDATE agents
SET tool_policy = jsonb_set(
        tool_policy,
        '{allow_tools}',
        (tool_policy->'allow_tools') - 'get_pipeline_status' - 'get_deploy_target'
    )
WHERE name = 'qa-agent'
  AND tool_policy->'allow_tools' ?| ARRAY['get_pipeline_status', 'get_deploy_target'];

-- product-manager: read-only code tools + stage deploy target.
UPDATE agents
SET tool_policy = jsonb_set(
        tool_policy,
        '{allow_tools}',
        (tool_policy->'allow_tools') - 'codebase_search' - 'grep_code' - 'get_repo_tree' - 'get_symbol_skeleton' - 'expand_symbol_context' - 'get_deploy_target'
    )
WHERE name = 'product-manager'
  AND tool_policy->'allow_tools' ?| ARRAY['codebase_search', 'grep_code', 'get_repo_tree', 'get_symbol_skeleton', 'expand_symbol_context', 'get_deploy_target'];
