-- Per-run tool counters, so a failing run leaves something the NEXT run can
-- read. Until now a run's tool failures existed only in the loop's memory and
-- in a debug-level log line: the retry started blind, and no KPI could see how
-- often an agent's tools were rejecting it.
ALTER TABLE task_agent_runs
    ADD COLUMN IF NOT EXISTS tool_calls INT NOT NULL DEFAULT 0,
    ADD COLUMN IF NOT EXISTS tool_errors INT NOT NULL DEFAULT 0,
    -- Which tools failed and how often, already rendered ("run_terminal failed
    -- 9×"). Stored as text rather than JSON because its only two consumers are
    -- a prompt line and a task comment, both of which want the sentence.
    ADD COLUMN IF NOT EXISTS error_pattern TEXT NOT NULL DEFAULT '';

-- The tool_error_rate KPI sums these per agent over a period.
CREATE INDEX IF NOT EXISTS idx_task_agent_runs_agent_created
    ON task_agent_runs (agent_id, created_at DESC);
