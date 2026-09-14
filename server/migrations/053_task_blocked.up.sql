-- Agent-asked-a-question blocking: when an agent cannot proceed without human
-- input it opens a clarification chat and parks the task here instead of
-- silently finishing the run. Answering in that chat clears the block and
-- re-dispatches the task, so the work resumes where it left off while the
-- agent is free to pick up other tasks in the meantime.

ALTER TABLE board_tasks
    ADD COLUMN IF NOT EXISTS blocked_question   TEXT,
    ADD COLUMN IF NOT EXISTS blocked_session_id UUID,
    ADD COLUMN IF NOT EXISTS blocked_at         TIMESTAMPTZ;

-- Answer lookup goes session -> task, and the board filters blocked tasks.
CREATE INDEX IF NOT EXISTS idx_board_tasks_blocked_session
    ON board_tasks (blocked_session_id) WHERE blocked_session_id IS NOT NULL;
