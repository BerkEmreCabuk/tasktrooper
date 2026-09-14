-- The product manager gets delete_board_task.
--
-- The PM could open tasks and move them, never remove one. Asked to merge three
-- tasks into one and delete the originals, it created a fourth task and left the
-- three standing — the only board write it had for "this should not exist" was
-- another write that added something.
--
-- Tool policies are written on agent CREATE only (admin customizations must
-- survive restarts), so existing installs need the tool appended: one guarded
-- UPDATE, skipped when the policy already carries it. Same pattern as
-- 073/074/076/082/087/091.
UPDATE agents
SET tool_policy = jsonb_set(
        tool_policy,
        '{allow_tools}',
        COALESCE(tool_policy->'allow_tools', '[]'::jsonb) || '["delete_board_task"]'::jsonb
    )
WHERE name = 'product-manager'
  AND NOT COALESCE(tool_policy->'allow_tools', '[]'::jsonb) ? 'delete_board_task';
