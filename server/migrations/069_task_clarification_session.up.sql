-- A task keeps one clarification chat for its whole life.
--
-- blocked_session_id is cleared the moment a human answers, so the next
-- question opened a brand-new chat: the thread holding the earlier questions
-- and answers was left behind, and neither the human nor the next agent run
-- could see what had already been settled. This column is set on the first
-- question and never cleared, so every later question continues that thread.
ALTER TABLE board_tasks
    ADD COLUMN IF NOT EXISTS clarification_session_id UUID;

-- Backfill from tasks blocked right now so an in-flight question keeps its chat.
UPDATE board_tasks
SET clarification_session_id = blocked_session_id
WHERE clarification_session_id IS NULL AND blocked_session_id IS NOT NULL;
