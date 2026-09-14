-- Written for symmetry with the rest of migrations/; nothing executes .down.sql
-- (migrations/embed.go embeds only *.up.sql and there is no rollback path).

DROP TABLE IF EXISTS deploy_package_tasks;
DROP TABLE IF EXISTS deploy_packages;

-- Narrow the relation-type check back to what migration 022 allowed. Any
-- deploy_depends_on rows must go first or the constraint cannot be re-added.
DELETE FROM task_relations WHERE relation_type = 'deploy_depends_on';
ALTER TABLE task_relations DROP CONSTRAINT IF EXISTS task_relations_relation_type_check;
ALTER TABLE task_relations ADD CONSTRAINT task_relations_relation_type_check
    CHECK (relation_type IN ('blocks'));

ALTER TABLE board_tasks DROP COLUMN IF EXISTS rollback_plan;
ALTER TABLE board_tasks DROP COLUMN IF EXISTS after_deploy;
ALTER TABLE board_tasks DROP COLUMN IF EXISTS before_deploy;
