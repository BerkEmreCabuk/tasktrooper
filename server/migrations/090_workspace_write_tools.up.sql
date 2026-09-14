-- Every agent that already holds the shell gets the file tools.
--
-- run_terminal was the only writer: each change went out as `sed -i` — one
-- file, one pattern, per agent turn, and silent about what it matched, so the
-- model spent a second turn grepping to find out whether the edit had landed.
-- Creating a file meant a heredoc, which allowlist mode rejects outright for
-- containing a backtick or $( , so an agent holding the shell still reported
-- that it could not create files.
--
-- Keyed on run_terminal, not on role names: these tools write exactly what the
-- shell can already write, confined to the workspace root, so an agent that has
-- the shell gains no reach it did not have — and an agent deliberately kept
-- away from it gains nothing at all. Roles added after this migration get them
-- from role_tools.go at create time.
--
-- Tool policies are written on agent CREATE only, so existing installs need the
-- tools appended: one guarded UPDATE per tool, skipped when the policy already
-- carries it, so re-running never appends a duplicate. Same pattern as
-- 073/074/076/082/087/089.
UPDATE agents
SET tool_policy = jsonb_set(
        tool_policy,
        '{allow_tools}',
        COALESCE(tool_policy->'allow_tools', '[]'::jsonb) || '["write_file"]'::jsonb
    )
WHERE COALESCE(tool_policy->'allow_tools', '[]'::jsonb) ? 'run_terminal'
  AND NOT COALESCE(tool_policy->'allow_tools', '[]'::jsonb) ? 'write_file';

UPDATE agents
SET tool_policy = jsonb_set(
        tool_policy,
        '{allow_tools}',
        COALESCE(tool_policy->'allow_tools', '[]'::jsonb) || '["edit_file"]'::jsonb
    )
WHERE COALESCE(tool_policy->'allow_tools', '[]'::jsonb) ? 'run_terminal'
  AND NOT COALESCE(tool_policy->'allow_tools', '[]'::jsonb) ? 'edit_file';

UPDATE agents
SET tool_policy = jsonb_set(
        tool_policy,
        '{allow_tools}',
        COALESCE(tool_policy->'allow_tools', '[]'::jsonb) || '["edit_lines"]'::jsonb
    )
WHERE COALESCE(tool_policy->'allow_tools', '[]'::jsonb) ? 'run_terminal'
  AND NOT COALESCE(tool_policy->'allow_tools', '[]'::jsonb) ? 'edit_lines';

UPDATE agents
SET tool_policy = jsonb_set(
        tool_policy,
        '{allow_tools}',
        COALESCE(tool_policy->'allow_tools', '[]'::jsonb) || '["delete_file"]'::jsonb
    )
WHERE COALESCE(tool_policy->'allow_tools', '[]'::jsonb) ? 'run_terminal'
  AND NOT COALESCE(tool_policy->'allow_tools', '[]'::jsonb) ? 'delete_file';

UPDATE agents
SET tool_policy = jsonb_set(
        tool_policy,
        '{allow_tools}',
        COALESCE(tool_policy->'allow_tools', '[]'::jsonb) || '["move_file"]'::jsonb
    )
WHERE COALESCE(tool_policy->'allow_tools', '[]'::jsonb) ? 'run_terminal'
  AND NOT COALESCE(tool_policy->'allow_tools', '[]'::jsonb) ? 'move_file';
