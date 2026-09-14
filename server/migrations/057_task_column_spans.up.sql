-- Time-in-column was not recorded anywhere: board_events holds the moves, but
-- answering "how long did QA hold this" meant replaying the whole event log per
-- task, and "which agent actually worked that column" was not answerable at all
-- (score events were written to the task's current assignee, which is the wrong
-- agent the moment a task changes hands).
--
-- A span is one uninterrupted stay in one column. Closing a span writes its
-- duration, so KPI queries never compute intervals at read time.

CREATE TABLE IF NOT EXISTS task_column_spans (
    id               UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    task_id          UUID NOT NULL REFERENCES board_tasks(id) ON DELETE CASCADE,
    repository_id    UUID NOT NULL REFERENCES repositories(id) ON DELETE CASCADE,
    board_column     TEXT NOT NULL,
    -- NULL while nobody has run yet, and permanently NULL for human-only
    -- columns (human_uat, analiz_review).
    agent_id         UUID REFERENCES agents(id) ON DELETE SET NULL,
    entered_at       TIMESTAMPTZ NOT NULL DEFAULT now(),
    left_at          TIMESTAMPTZ,
    duration_seconds INT,
    -- A need_revision loop re-enters the same column; each visit is its own row.
    visit_no         INT NOT NULL DEFAULT 1,
    -- Set on review columns when the reviewing agent approves or rejects.
    review_verdict   TEXT
);

CREATE INDEX IF NOT EXISTS idx_task_column_spans_task
    ON task_column_spans(task_id, entered_at);
CREATE INDEX IF NOT EXISTS idx_task_column_spans_agent
    ON task_column_spans(agent_id, board_column, left_at DESC);
-- Finding the single open span of a task is the hottest lookup on the write path.
CREATE INDEX IF NOT EXISTS idx_task_column_spans_open
    ON task_column_spans(task_id) WHERE left_at IS NULL;

-- Speed is measured only over tasks that finished without rework, so both the
-- verdict and the moment it was reached have to be stamped on the task.
ALTER TABLE board_tasks ADD COLUMN IF NOT EXISTS clean_completion BOOLEAN NOT NULL DEFAULT false;
ALTER TABLE board_tasks ADD COLUMN IF NOT EXISTS completed_at TIMESTAMPTZ;

-- Backfill spans from the recorded move history. Idempotent: skipped entirely
-- once the table holds anything.
DO $$
BEGIN
    IF EXISTS (SELECT 1 FROM task_column_spans LIMIT 1) THEN
        RETURN;
    END IF;

    INSERT INTO task_column_spans (
        task_id, repository_id, board_column, agent_id,
        entered_at, left_at, duration_seconds, visit_no
    )
    SELECT
        m.task_id,
        m.repository_id,
        m.col,
        r.agent_id,
        m.created_at,
        m.next_at,
        CASE WHEN m.next_at IS NULL THEN NULL
             ELSE GREATEST(0, EXTRACT(EPOCH FROM (m.next_at - m.created_at))::int)
        END,
        ROW_NUMBER() OVER (PARTITION BY m.task_id, m.col ORDER BY m.created_at)
    FROM (
        SELECT
            e.id,
            e.task_id,
            e.repository_id,
            e.created_at,
            COALESCE(e.payload->>'column', e.payload->>'to_column') AS col,
            LEAD(e.created_at) OVER (PARTITION BY e.task_id ORDER BY e.created_at) AS next_at
        FROM board_events e
        WHERE e.event_type IN ('task.moved', 'task.created')
          AND COALESCE(e.payload->>'column', e.payload->>'to_column') IS NOT NULL
    ) m
    LEFT JOIN LATERAL (
        SELECT tar.agent_id
        FROM task_agent_runs tar
        WHERE tar.board_event_id = m.id
        ORDER BY tar.created_at
        LIMIT 1
    ) r ON true;

    -- Historic tasks already sitting in done/released get a completion stamp so
    -- they are not silently treated as still open. They stay marked unclean:
    -- the reconstructed history cannot prove they never bounced.
    UPDATE board_tasks
    SET completed_at = COALESCE(completed_at, updated_at)
    WHERE board_column IN ('done', 'released');
END $$;
