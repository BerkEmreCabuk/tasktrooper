-- Runtime self-skill authoring: agents whose work has no matching skill can
-- write themselves one with the new create_skill tool (gated per-agent by
-- self_evolution_enabled at execution time). Tool policies are written on
-- agent CREATE only, so existing installs need the tool backfilled into every
-- policy that already carries load_skill — same pattern as migration 073's
-- review_criterion backfill.
UPDATE agents
SET tool_policy = jsonb_set(
        tool_policy,
        '{allow_tools}',
        COALESCE(tool_policy->'allow_tools', '[]'::jsonb) || '["create_skill"]'::jsonb
    )
WHERE COALESCE(tool_policy->'allow_tools', '[]'::jsonb) ? 'load_skill'
  AND NOT COALESCE(tool_policy->'allow_tools', '[]'::jsonb) ? 'create_skill';
