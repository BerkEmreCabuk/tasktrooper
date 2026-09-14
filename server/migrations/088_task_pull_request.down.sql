-- Written for symmetry with the rest of migrations/; nothing executes .down.sql
-- (migrations/embed.go embeds only *.up.sql and there is no rollback path).

-- Scoped by agent name, like 081's and 085's down: only the roles this
-- migration granted a tool to lose it.
UPDATE agents
SET tool_policy = jsonb_set(tool_policy, '{allow_tools}',
        (tool_policy->'allow_tools') - 'commit_task_changes')
WHERE name IN ('backend-developer', 'frontend-developer', 'mobile-developer')
  AND tool_policy->'allow_tools' ? 'commit_task_changes';

UPDATE agents
SET tool_policy = jsonb_set(tool_policy, '{allow_tools}',
        (tool_policy->'allow_tools') - 'comment_on_pull_request')
WHERE name IN ('backend-developer', 'frontend-developer', 'mobile-developer', 'system-architect')
  AND tool_policy->'allow_tools' ? 'comment_on_pull_request';

UPDATE agents
SET tool_policy = jsonb_set(tool_policy, '{allow_tools}',
        (tool_policy->'allow_tools') - 'get_task_pull_request')
WHERE name IN ('backend-developer', 'frontend-developer', 'mobile-developer',
               'system-architect', 'qa-agent', 'product-manager')
  AND tool_policy->'allow_tools' ? 'get_task_pull_request';

DROP INDEX IF EXISTS idx_sessions_task;
ALTER TABLE sessions DROP COLUMN IF EXISTS task_id;

ALTER TABLE board_tasks DROP COLUMN IF EXISTS pr_number;
ALTER TABLE board_tasks DROP COLUMN IF EXISTS pr_url;
