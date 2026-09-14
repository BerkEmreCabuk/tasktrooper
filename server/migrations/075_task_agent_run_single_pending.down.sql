-- Only the constraint is reversible. The duplicate queued runs the up
-- migration cancelled stay cancelled: re-queueing them would hand the board
-- back the very pile-up the index exists to prevent, and nothing recorded
-- which cancellation belonged to which racer.
DROP INDEX IF EXISTS idx_task_agent_runs_one_pending;
