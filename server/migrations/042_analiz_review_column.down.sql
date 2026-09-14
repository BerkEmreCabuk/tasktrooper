-- Reverse 042: remove the analiz_review column and its transitions.

-- 1. Drop analiz_review transition edges.
DELETE FROM board_column_transitions
WHERE from_slug = 'analiz_review' OR to_slug = 'analiz_review';

-- 2. Remove the column and close the position gap.
DO $$
DECLARE
    ip INT;
BEGIN
    SELECT position INTO ip FROM board_columns WHERE slug = 'in_progress';
    IF ip IS NULL THEN
        ip := 2;
    END IF;
    DELETE FROM board_columns WHERE slug = 'analiz_review';
    UPDATE board_columns SET position = position - 1 WHERE position > ip;
END $$;
