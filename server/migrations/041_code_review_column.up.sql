-- Add the code_review column between in_progress and ready_for_qa. The
-- system-architect agent gates this column: the build/test pipeline runs on
-- entry, then the architect reviews the branch diff before ready_for_qa.

-- 1. Insert the column after in_progress, shifting later columns down by one.
--    Idempotent: skips if code_review already exists.
DO $$
DECLARE
    ip INT;
BEGIN
    SELECT position INTO ip FROM board_columns WHERE slug = 'in_progress';
    IF ip IS NULL THEN
        ip := 2;
    END IF;
    IF NOT EXISTS (SELECT 1 FROM board_columns WHERE slug = 'code_review') THEN
        UPDATE board_columns SET position = position + 1 WHERE position > ip;
        INSERT INTO board_columns (slug, label, position, is_backlog)
        VALUES ('code_review', 'Code Review', ip + 1, false);
    END IF;
END $$;

-- 2. Rewire allowed transitions ONLY for installs that use a restricted
--    transition graph (detected by an existing in_progress -> ready_for_qa
--    edge). Installs with no transitions stay fully permissive, so nothing
--    else is constrained. New flow: in_progress -> code_review -> ready_for_qa,
--    with need_revision reachable from code_review.
DO $$
BEGIN
    IF EXISTS (
        SELECT 1 FROM board_column_transitions
        WHERE from_slug = 'in_progress' AND to_slug = 'ready_for_qa'
    ) THEN
        DELETE FROM board_column_transitions
        WHERE from_slug = 'in_progress' AND to_slug = 'ready_for_qa';
        INSERT INTO board_column_transitions (from_slug, to_slug) VALUES
            ('in_progress', 'code_review'),
            ('code_review', 'ready_for_qa'),
            ('code_review', 'need_revision')
        ON CONFLICT DO NOTHING;
    END IF;
END $$;
