-- Remove delete_board_task from the role 092 gave it to.
UPDATE agents
SET tool_policy = jsonb_set(
        tool_policy,
        '{allow_tools}',
        COALESCE(tool_policy->'allow_tools', '[]'::jsonb) - 'delete_board_task'
    )
WHERE name = 'product-manager';
