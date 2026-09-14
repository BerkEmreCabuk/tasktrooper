-- A run executed by the local Claude Code CLI (agents on provider
-- 'claude_code') can be stopped by the subscription's usage limit rather than
-- by anything wrong with the work. That is a park, not a failure — the board
-- already parks tasks on a held test device the same way, through
-- board_tasks.blocked_resource — but the device sweeper's question ("is the
-- phone free") is answered by hardware, while this one is answered by a clock
-- the run itself was told about.
--
-- So the two facts the resume needs live on the run row:
--
--   quota_resume_at  when the limit is expected to lift, parsed from the CLI's
--                    own "Claude AI usage limit reached|<epoch>" message (or
--                    now + domain.DefaultQuotaParkWindow when it said no time).
--                    NULL on every run that was never parked.
--   cli_session_id   the parked CLI session, replayed with `claude -p --resume
--                    <id>` so the continuation keeps the exploration and the
--                    half-written change the first attempt already paid for.
--                    Kept AFTER the resume too: it is what a second park
--                    resumes from, and it is the only trace of which CLI
--                    session did the work.
--
-- On the row rather than in memory because a parked run must survive a restart:
-- the sweeper reads the database, so a pod that died between the park and the
-- reset still wakes the task.
ALTER TABLE task_agent_runs
    ADD COLUMN IF NOT EXISTS cli_session_id  TEXT        NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS quota_resume_at TIMESTAMPTZ;

-- Partial: parked runs are a tiny minority of the table and the sweeper's only
-- question is "is any of them due". A full index would be mostly NULLs and
-- would be paid for on every run insert and update.
CREATE INDEX IF NOT EXISTS idx_task_agent_runs_quota_resume
    ON task_agent_runs (quota_resume_at)
    WHERE quota_resume_at IS NOT NULL;
