-- Task numbers were MAX(task_number) + 1, computed over the live rows: deleting
-- the newest task handed its number to the next one created, so a key could
-- name two different tasks over time — in a board where the key is what a PR
-- title, a branch, a commit trailer and a chat all refer to.
--
-- The counter is stored instead, one per task type, and only ever goes up.
-- Per type, because the key now carries the type (T-1 work, B-1 bug, A-1
-- analysis) and three interleaved sequences would make the numbers meaningless.
CREATE TABLE IF NOT EXISTS board_task_counters (
	task_type   TEXT PRIMARY KEY,
	last_number BIGINT NOT NULL DEFAULT 0
);

-- Seed from what exists so no new key can collide with a task already out
-- there: numbers used to be board-wide, so per type the maximum is the safe
-- floor. Tasks keep the numbers they have; only their prefix changes.
INSERT INTO board_task_counters (task_type, last_number)
SELECT task_type, MAX(task_number) FROM board_tasks GROUP BY task_type
ON CONFLICT (task_type) DO UPDATE
	SET last_number = GREATEST(board_task_counters.last_number, EXCLUDED.last_number);

-- The three known types exist even on an empty board, so the first insert of
-- each kind is an ordinary increment rather than a special case.
INSERT INTO board_task_counters (task_type, last_number)
VALUES ('task', 0), ('bug', 0), ('analiz', 0)
ON CONFLICT (task_type) DO NOTHING;

-- task_number was globally unique because the key was one board-wide prefix
-- plus the number. Now the prefix is the type, so T-1 and B-1 are two different
-- tasks and the uniqueness that matters is per type. Existing rows satisfy this
-- already — a globally unique number is unique within its type too.
DROP INDEX IF EXISTS idx_board_tasks_task_number;
CREATE UNIQUE INDEX IF NOT EXISTS idx_board_tasks_type_task_number
	ON board_tasks(task_type, task_number);
