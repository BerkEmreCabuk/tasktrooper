UPDATE agents
SET tool_policy = jsonb_set(
        tool_policy,
        '{allow_tools}',
        (tool_policy->'allow_tools') - 'create_skill'
    )
WHERE tool_policy->'allow_tools' ? 'create_skill';
