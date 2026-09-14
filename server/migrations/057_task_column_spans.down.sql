DROP TABLE IF EXISTS task_column_spans;
ALTER TABLE board_tasks DROP COLUMN IF EXISTS clean_completion;
ALTER TABLE board_tasks DROP COLUMN IF EXISTS completed_at;
