-- Written for symmetry with the rest of migrations/; nothing executes .down.sql
-- (migrations/embed.go embeds only *.up.sql and there is no rollback path).

UPDATE agents
SET tool_policy = jsonb_set(tool_policy, '{allow_tools}',
        ((tool_policy->'allow_tools') - 'update_task_document'))
WHERE COALESCE(tool_policy->'allow_tools', '[]'::jsonb) ? 'update_task_document';
