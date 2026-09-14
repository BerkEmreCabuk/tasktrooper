DELETE FROM task_comments WHERE author_type = 'system';

ALTER TABLE task_comments DROP CONSTRAINT IF EXISTS task_comments_author_type_check;
ALTER TABLE task_comments ADD CONSTRAINT task_comments_author_type_check
    CHECK (author_type IN ('user', 'agent'));
