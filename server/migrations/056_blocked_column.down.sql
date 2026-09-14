-- Reverse 056: return blocked tasks to the column they came from, then drop the
-- column and the origin bookkeeping.

UPDATE board_tasks
SET board_column = COALESCE(NULLIF(blocked_origin_column, ''), 'todo'),
    updated_at   = now()
WHERE board_column = 'blocked';

DELETE FROM board_column_transitions
WHERE from_slug = 'blocked' OR to_slug = 'blocked';

DO $$
DECLARE
    blocked_pos INT;
BEGIN
    SELECT position INTO blocked_pos FROM board_columns WHERE slug = 'blocked';
    IF blocked_pos IS NOT NULL THEN
        DELETE FROM board_columns WHERE slug = 'blocked';
        UPDATE board_columns SET position = position - 1 WHERE position > blocked_pos;
    END IF;
END $$;

ALTER TABLE board_tasks DROP COLUMN IF EXISTS blocked_origin_column;
