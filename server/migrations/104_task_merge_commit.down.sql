-- Written for symmetry with the rest of migrations/; nothing executes .down.sql
-- (migrations/embed.go embeds only *.up.sql and there is no rollback path).

-- Scoped by agent name, like 088's down: only the role this migration granted
-- the tool to loses it.
UPDATE agents
SET tool_policy = jsonb_set(tool_policy, '{allow_tools}',
        (tool_policy->'allow_tools') - 'merge_task_pull_request')
WHERE name = 'qa-agent'
  AND tool_policy->'allow_tools' ? 'merge_task_pull_request';

DELETE FROM agent_column_subscriptions s
USING agents a
WHERE a.id = s.agent_id
  AND a.name = 'qa-agent'
  AND s.column_slug = 'done';

ALTER TABLE board_tasks DROP COLUMN IF EXISTS merge_commit_sha;
