-- Written for symmetry with the rest of migrations/; nothing executes .down.sql
-- (migrations/embed.go embeds only *.up.sql and there is no rollback path).

DELETE FROM task_relations WHERE relation_type = 'discovered_from';

ALTER TABLE task_relations DROP CONSTRAINT IF EXISTS task_relations_relation_type_check;
ALTER TABLE task_relations ADD CONSTRAINT task_relations_relation_type_check
    CHECK (relation_type IN ('blocks', 'deploy_depends_on', 'derived_from'));
