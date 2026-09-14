-- Every agent that can grep the repository can now also read a file.
--
-- There was no read tool at all: reading meant run_terminal, so agents read
-- source with `sed -n '630,640p'` and slid a ten-line window down the file —
-- one LLM round-trip and one full context replay per ten lines. A run spent
-- sixty of its eighty iterations scrolling and never reached its edit.
--
-- Tool policies are written on agent CREATE only (admin customizations must
-- survive restarts), so existing installs need the tool appended. The run-time
-- uplift already grants CodeExplorationTools to chat and board runs, but an
-- orchestrated subtask is restricted to its agent's persisted list — this is
-- what reaches that path. Guarded so re-running never appends a duplicate;
-- same pattern as 073/074/076/082/087.
--
-- Keyed on grep_code rather than on role names: whoever may search the
-- repository may read it, including roles added after this migration was
-- written and installs whose policies an admin has customised. read_file is
-- confined to the workspace root exactly as grep_code is, so this widens no
-- agent's reach.
UPDATE agents
SET tool_policy = jsonb_set(
        tool_policy,
        '{allow_tools}',
        COALESCE(tool_policy->'allow_tools', '[]'::jsonb) || '["read_file"]'::jsonb
    )
WHERE COALESCE(tool_policy->'allow_tools', '[]'::jsonb) ? 'grep_code'
  AND NOT COALESCE(tool_policy->'allow_tools', '[]'::jsonb) ? 'read_file';
