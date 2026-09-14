-- Written for symmetry with the rest of migrations/; nothing executes .down.sql
-- (migrations/embed.go embeds only *.up.sql and there is no rollback path).

UPDATE agents
SET tool_policy = jsonb_set(tool_policy, '{allow_tools}',
        ((tool_policy->'allow_tools') - 'get_task_deploy_status' - 'get_deploy_logs' - 'rollback_task_release'))
WHERE name = 'qa-agent';

DELETE FROM agent_column_subscriptions s
USING agents a
WHERE a.id = s.agent_id
  AND a.name = 'qa-agent'
  AND s.column_slug = 'released';

DROP INDEX IF EXISTS idx_board_tasks_merge_commit;

ALTER TABLE repository_deploy_targets DROP COLUMN IF EXISTS logs_url;
