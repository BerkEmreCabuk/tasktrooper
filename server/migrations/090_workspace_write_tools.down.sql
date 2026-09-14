-- Take the workspace file tools back off every policy that carries them.
-- Agents fall back to writing through run_terminal, which is what they did
-- before these existed.
UPDATE agents
SET tool_policy = jsonb_set(
        tool_policy,
        '{allow_tools}',
        COALESCE(tool_policy->'allow_tools', '[]'::jsonb)
            - 'write_file' - 'edit_file' - 'edit_lines' - 'delete_file' - 'move_file'
    )
WHERE COALESCE(tool_policy->'allow_tools', '[]'::jsonb)
        ?| ARRAY['write_file', 'edit_file', 'edit_lines', 'delete_file', 'move_file'];
