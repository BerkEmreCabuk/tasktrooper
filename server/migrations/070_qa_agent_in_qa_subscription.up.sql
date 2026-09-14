-- QA owns both of its columns: ready_for_qa is the queue it is handed, in_qa is
-- where it tests.
--
-- The role seed only sets subscriptions for an agent that has none, so every
-- install created before this change has qa-agent on ready_for_qa alone. That
-- left in_qa unowned: a task moved there resolved back to the implementer, and
-- the developer was dispatched onto the branch QA had just started testing.
INSERT INTO agent_column_subscriptions (agent_id, column_slug, task_type_filter)
SELECT s.agent_id, 'in_qa', s.task_type_filter
FROM agent_column_subscriptions s
JOIN agents a ON a.id = s.agent_id
WHERE s.column_slug = 'ready_for_qa'
  AND a.name = 'qa-agent'
ON CONFLICT (agent_id, column_slug) DO NOTHING;
