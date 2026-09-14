-- Take the browser tools back off the developer roles. Only those roles are
-- touched: qa-agent and product-manager carry the same tools from their own
-- seed and must keep them.
UPDATE agents
SET tool_policy = jsonb_set(
        tool_policy,
        '{allow_tools}',
        COALESCE(tool_policy->'allow_tools', '[]'::jsonb) - 'browser_navigate'
            - 'browser_screenshot' - 'browser_click' - 'browser_fill'
            - 'browser_read_dom' - 'browser_wait_for'
    )
WHERE name IN ('backend-developer', 'frontend-developer', 'mobile-developer');
