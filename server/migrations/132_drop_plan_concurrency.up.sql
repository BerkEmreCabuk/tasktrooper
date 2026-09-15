-- The plan no longer caps how many tasks run at once: nothing reads this
-- column since the run claim stopped counting live runs.
ALTER TABLE billing_plan DROP COLUMN IF EXISTS max_concurrent_tasks;
