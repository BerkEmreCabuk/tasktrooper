DROP INDEX IF EXISTS idx_board_tasks_blocked_session;

ALTER TABLE board_tasks
    DROP COLUMN IF EXISTS blocked_question,
    DROP COLUMN IF EXISTS blocked_session_id,
    DROP COLUMN IF EXISTS blocked_at;
