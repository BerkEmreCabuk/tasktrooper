-- Promote "waiting on a human answer" from a flag to a real board column.
--
-- Migration 053 parked a blocked task in place, marked only by blocked_at. That
-- kept the task counted as work in flight: an agent that cannot proceed still
-- inflated the in_progress/code_review KPIs, so "how much is actually moving"
-- could not be answered from the board. A task nobody can advance belongs in its
-- own column.
--
-- The column the task came from is recorded so answering returns it to exactly
-- where it stopped, rather than to a guessed default.

ALTER TABLE board_tasks
    ADD COLUMN IF NOT EXISTS blocked_origin_column TEXT;

-- Place Blocked just before Done: it is off the happy path, and grouping it with
-- the terminal columns keeps the left-to-right flow of active work unbroken.
DO $$
DECLARE
    done_pos INT;
BEGIN
    IF NOT EXISTS (SELECT 1 FROM board_columns WHERE slug = 'blocked') THEN
        SELECT position INTO done_pos FROM board_columns WHERE slug = 'done';
        IF done_pos IS NULL THEN
            SELECT COALESCE(MAX(position), 0) + 1 INTO done_pos FROM board_columns;
        END IF;
        UPDATE board_columns SET position = position + 1 WHERE position >= done_pos;
        INSERT INTO board_columns (slug, label, position, is_backlog)
        VALUES ('blocked', 'Blocked', done_pos, false);
    END IF;
END $$;

-- Only installs running a restricted transition graph need edges; an install
-- with no transitions stays fully permissive. A task can enter blocked from any
-- active column and return to it, so mirror every existing outgoing edge.
DO $$
BEGIN
    IF EXISTS (SELECT 1 FROM board_column_transitions) THEN
        INSERT INTO board_column_transitions (from_slug, to_slug)
        SELECT DISTINCT c.slug, 'blocked'
        FROM board_columns c
        WHERE c.slug NOT IN ('blocked', 'backlog')
        ON CONFLICT DO NOTHING;

        INSERT INTO board_column_transitions (from_slug, to_slug)
        SELECT DISTINCT 'blocked', c.slug
        FROM board_columns c
        WHERE c.slug NOT IN ('blocked', 'backlog')
        ON CONFLICT DO NOTHING;
    END IF;
END $$;

-- Tasks parked by 053 are still sitting in their original column. Move them into
-- the new column, remembering where they came from.
UPDATE board_tasks
SET blocked_origin_column = board_column,
    board_column          = 'blocked',
    updated_at            = now()
WHERE blocked_at IS NOT NULL
  AND board_column <> 'blocked';
