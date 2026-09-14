-- Removal is scoped by agent name, mirroring 076's down: other roles never
-- received attach_task_file from this migration.
UPDATE agents
SET tool_policy = jsonb_set(
        tool_policy,
        '{allow_tools}',
        (tool_policy->'allow_tools') - 'attach_task_file'
    )
WHERE name IN ('product-manager', 'system-architect')
  AND tool_policy->'allow_tools' ? 'attach_task_file';

DROP INDEX IF EXISTS idx_session_message_attachments_attachment;
DROP INDEX IF EXISTS idx_task_attachments_attachment;

DROP TABLE IF EXISTS session_message_attachments;
DROP TABLE IF EXISTS task_attachments;
DROP TABLE IF EXISTS attachments;
