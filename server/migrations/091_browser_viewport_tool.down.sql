-- Remove browser_set_viewport from the roles 091 gave it to.
UPDATE agents
SET tool_policy = jsonb_set(
        tool_policy,
        '{allow_tools}',
        COALESCE(tool_policy->'allow_tools', '[]'::jsonb) - 'browser_set_viewport'
    )
WHERE name IN ('backend-developer', 'frontend-developer', 'mobile-developer', 'qa-agent', 'product-manager');
