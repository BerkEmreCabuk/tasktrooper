-- Board automation writes comments as author_type 'system' (pipeline results,
-- verify-gate notes, prod-gate warnings, "run stopped before finishing"), but
-- the check constraint only ever allowed 'user' and 'agent'. Every one of those
-- inserts was rejected by the database and swallowed as a log warning, so the
-- board silently lost every explanation the system tried to leave on a task.
--
-- The constraint kept its pre-rename name (022 renamed the table but not the
-- constraint), so both spellings are dropped before the widened one is added.
ALTER TABLE task_comments DROP CONSTRAINT IF EXISTS project_task_comments_author_type_check;
ALTER TABLE task_comments DROP CONSTRAINT IF EXISTS task_comments_author_type_check;
ALTER TABLE task_comments ADD CONSTRAINT task_comments_author_type_check
    CHECK (author_type IN ('user', 'agent', 'system'));
