-- Add the analiz_review column between in_progress and code_review. This is a
-- HUMAN review gate: the system-architect moves a finished analiz task (spec +
-- implementation plan) into analiz_review and stops. The human reviews the
-- plan and either approves it (move to done -> architect creates the
-- implementation tasks and releases) or rejects it (move to need_revision ->
-- architect reworks the plan). No agent is subscribed to analiz_review.

-- 1. Insert the column after in_progress, shifting later columns down by one.
--    Idempotent: skips if analiz_review already exists.
DO $$
DECLARE
    ip INT;
BEGIN
    SELECT position INTO ip FROM board_columns WHERE slug = 'in_progress';
    IF ip IS NULL THEN
        ip := 2;
    END IF;
    IF NOT EXISTS (SELECT 1 FROM board_columns WHERE slug = 'analiz_review') THEN
        UPDATE board_columns SET position = position + 1 WHERE position > ip;
        INSERT INTO board_columns (slug, label, position, is_backlog)
        VALUES ('analiz_review', 'Analiz Review', ip + 1, false);
    END IF;
END $$;

-- 2. Rewire allowed transitions ONLY for installs that use a restricted
--    transition graph (detected by the in_progress -> code_review edge that
--    migration 041 installs). Installs with no transitions stay fully
--    permissive, so nothing else is constrained. New analiz flow:
--    in_progress -> analiz_review -> done (approve) | need_revision (reject).
DO $$
BEGIN
    IF EXISTS (
        SELECT 1 FROM board_column_transitions
        WHERE from_slug = 'in_progress' AND to_slug = 'code_review'
    ) THEN
        INSERT INTO board_column_transitions (from_slug, to_slug) VALUES
            ('in_progress', 'analiz_review'),
            ('analiz_review', 'done'),
            ('analiz_review', 'need_revision')
        ON CONFLICT DO NOTHING;
    END IF;
END $$;
