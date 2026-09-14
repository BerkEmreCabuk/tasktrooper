-- Removal is scoped by agent name, mirroring 076's down: only the four roles
-- this migration's up touched can carry the tool, so nothing else is stripped.
UPDATE agents
SET tool_policy = jsonb_set(
        tool_policy,
        '{allow_tools}',
        (tool_policy->'allow_tools') - 'update_project_profile'
    )
WHERE name IN ('system-architect', 'backend-developer', 'frontend-developer', 'mobile-developer')
  AND tool_policy->'allow_tools' ? 'update_project_profile';

ALTER TABLE repositories
    DROP COLUMN IF EXISTS profile_md,
    DROP COLUMN IF EXISTS profile_updated_at;
