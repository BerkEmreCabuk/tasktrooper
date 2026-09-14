-- Per-repository "project profile": an agent-maintained markdown brief of the
-- codebase (stack, layout, commands, conventions) injected into every
-- repo-scoped agent run so agents stop re-deriving the basics each time.
-- profile_updated_at drives the push-webhook staleness gate (refresh only when
-- NULL or older than the freshness window).
ALTER TABLE repositories
    ADD COLUMN profile_md TEXT,
    ADD COLUMN profile_updated_at TIMESTAMPTZ;

-- Backfill the new update_project_profile tool into the roles that maintain
-- the profile. Tool policies are written on agent CREATE only (admin
-- customizations must survive restarts), so existing installs need the tool
-- appended — same guarded-append pattern as migrations 073/074/076: one UPDATE
-- per tool, skipped when the policy already carries it, so re-running never
-- appends a duplicate.
UPDATE agents
SET tool_policy = jsonb_set(
        tool_policy,
        '{allow_tools}',
        COALESCE(tool_policy->'allow_tools', '[]'::jsonb) || '["update_project_profile"]'::jsonb
    )
WHERE name IN ('system-architect', 'backend-developer', 'frontend-developer', 'mobile-developer')
  AND NOT COALESCE(tool_policy->'allow_tools', '[]'::jsonb) ? 'update_project_profile';
