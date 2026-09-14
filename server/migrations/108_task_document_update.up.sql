-- update_task_document, for every agent that can already write one.
--
-- Tool policies are written on agent CREATE only (an admin's customization must
-- survive a restart), so existing installs need new tools backfilled here — the
-- same guarded-UPDATE pattern as migrations 081, 085, 088, 104, 105 and 106:
-- re-running never appends a duplicate.
--
-- Keyed on add_task_document rather than on a role name. An agent trusted to
-- attach a spec or a plan to a card is the agent that will be asked to change
-- it, and until this tool existed the only call available for that was
-- add_task_document again — so a revision arrived as "Spec v2" beside "Spec",
-- and whoever picked the task up had to guess which one was current.
UPDATE agents
SET tool_policy = jsonb_set(
        tool_policy,
        '{allow_tools}',
        COALESCE(tool_policy->'allow_tools', '[]'::jsonb) || '["update_task_document"]'::jsonb
    )
WHERE COALESCE(tool_policy->'allow_tools', '[]'::jsonb) ? 'add_task_document'
  AND NOT COALESCE(tool_policy->'allow_tools', '[]'::jsonb) ? 'update_task_document';
