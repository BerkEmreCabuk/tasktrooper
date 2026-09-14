-- Take read_file back off every policy that carries it. Agents fall back to
-- reading through run_terminal, which is what they did before it existed.
UPDATE agents
SET tool_policy = jsonb_set(
        tool_policy,
        '{allow_tools}',
        COALESCE(tool_policy->'allow_tools', '[]'::jsonb) - 'read_file'
    )
WHERE COALESCE(tool_policy->'allow_tools', '[]'::jsonb) ? 'read_file';
