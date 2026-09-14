-- Written for symmetry with the rest of migrations/; nothing executes .down.sql
-- (migrations/embed.go embeds only *.up.sql and there is no rollback path).

UPDATE agents
SET tool_policy = jsonb_set(tool_policy, '{allow_tools}',
        ((tool_policy->'allow_tools') - 'list_task_documents'))
WHERE COALESCE(tool_policy->'allow_tools', '[]'::jsonb) ? 'list_task_documents';

DROP INDEX IF EXISTS idx_task_relations_target_type;

DELETE FROM task_relations WHERE relation_type = 'derived_from';

ALTER TABLE task_relations DROP CONSTRAINT IF EXISTS task_relations_relation_type_check;
ALTER TABLE task_relations ADD CONSTRAINT task_relations_relation_type_check
    CHECK (relation_type IN ('blocks', 'deploy_depends_on'));
