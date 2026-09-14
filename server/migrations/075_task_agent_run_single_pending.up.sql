-- At most one QUEUED run per (task, agent), enforced by the database.
--
-- Dispatch reads HasPendingForTask and, if nothing is queued, inserts — two
-- round-trips with no lock between them. Two board events for the same
-- task+agent landing in that window (the task.assigned the UI fires right
-- behind a task.moved, a webhook arriving alongside a UI action) both read
-- "nothing queued" and both insert. The runner serialises execution per task,
-- so the duplicate does not run concurrently; it queues, burns a second
-- agent/LLM run, and then executes against a task the first run has already
-- advanced past, with plan text that no longer matches.
--
-- The predicate is 'pending' ONLY, deliberately not the whole non-terminal set
-- ('pending','running'): a run that ends by pushing its task back — the verify
-- gate returning it to in_progress — has to be able to queue the follow-up fix
-- round while it is itself still marked running. That is the documented
-- contract of HasPendingForTask, and including 'running' in this predicate
-- would swallow the follow-up and park the task in its column with no agent.
-- 'pending' matches HasPendingForTask exactly, so the index and the read it
-- backstops agree on what counts as a duplicate.

-- Existing duplicates have to go before the index can be built, and this bug
-- has been live, so they exist. The oldest queued row per (task, agent) is the
-- one the runner would have picked up first, so it survives. The rest are
-- marked cancelled, not failed: nothing went wrong, they should never have
-- been created — and 'failed' would feed both the reconciler's
-- consecutive-failure count and the error pattern the next run inherits.
WITH duplicates AS (
    SELECT id
    FROM (
        SELECT id, ROW_NUMBER() OVER (
            PARTITION BY task_id, agent_id
            ORDER BY created_at, id
        ) AS rn
        FROM task_agent_runs
        WHERE status = 'pending'
    ) ranked
    WHERE rn > 1
)
UPDATE task_agent_runs SET
    status = 'cancelled',
    summary = CASE
        WHEN summary = '' THEN 'duplicate queued run, superseded by the run that won the dispatch race'
        ELSE summary
    END,
    updated_at = now()
WHERE id IN (SELECT id FROM duplicates);

CREATE UNIQUE INDEX IF NOT EXISTS idx_task_agent_runs_one_pending
    ON task_agent_runs (task_id, agent_id)
    WHERE status = 'pending';
