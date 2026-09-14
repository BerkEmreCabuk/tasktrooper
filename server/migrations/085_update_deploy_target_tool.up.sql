-- update_deploy_target for QA and PM. Tool policies are written on agent
-- CREATE only (admin customizations must survive restarts), so existing
-- installs need the new tool backfilled — same guarded-UPDATE pattern as
-- migration 076: re-running never appends a duplicate.
--
-- The tool can only write a target's base_url/health_url, which is exactly the
-- gap it closes: nothing set those automatically, so a repo's first stage or
-- prod deploy left the address it actually served recorded nowhere and every
-- later role had to rediscover it.

UPDATE agents
SET tool_policy = jsonb_set(
        tool_policy,
        '{allow_tools}',
        COALESCE(tool_policy->'allow_tools', '[]'::jsonb) || '["update_deploy_target"]'::jsonb
    )
WHERE name IN ('qa-agent', 'product-manager')
  AND NOT COALESCE(tool_policy->'allow_tools', '[]'::jsonb) ? 'update_deploy_target';
