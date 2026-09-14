-- Reverse 041: remove the code_review column and its transitions.

-- 1. Drop code_review transition edges; restore the direct in_progress ->
--    ready_for_qa edge only where a restricted graph is in use.
DELETE FROM board_column_transitions
WHERE from_slug = 'code_review' OR to_slug = 'code_review';

DO $$
BEGIN
    IF EXISTS (SELECT 1 FROM board_column_transitions WHERE from_slug = 'in_progress') THEN
        INSERT INTO board_column_transitions (from_slug, to_slug)
        VALUES ('in_progress', 'ready_for_qa')
        ON CONFLICT DO NOTHING;
    END IF;
END $$;

-- 2. Remove the column and close the position gap.
DO $$
DECLARE
    ip INT;
BEGIN
    SELECT position INTO ip FROM board_columns WHERE slug = 'in_progress';
    IF ip IS NULL THEN
        ip := 2;
    END IF;
    DELETE FROM board_columns WHERE slug = 'code_review';
    UPDATE board_columns SET position = position - 1 WHERE position > ip;
END $$;
