DROP TABLE IF EXISTS board_task_counters;
DROP INDEX IF EXISTS idx_board_tasks_type_task_number;
CREATE UNIQUE INDEX IF NOT EXISTS idx_board_tasks_task_number ON board_tasks(task_number);
