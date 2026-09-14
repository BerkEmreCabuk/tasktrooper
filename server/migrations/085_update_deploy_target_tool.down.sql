-- Scoped by agent name, like 076's down: only the two roles this migration
-- granted the tool to lose it.

UPDATE agents
SET tool_policy = jsonb_set(
        tool_policy,
        '{allow_tools}',
        (tool_policy->'allow_tools') - 'update_deploy_target'
    )
WHERE name IN ('qa-agent', 'product-manager')
  AND tool_policy->'allow_tools' ? 'update_deploy_target';
