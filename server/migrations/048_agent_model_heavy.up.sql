-- Optional stronger model per agent, used for subtasks the planner rates "hard".
-- Empty string = always use the agent's default model.
ALTER TABLE agents ADD COLUMN IF NOT EXISTS model_heavy TEXT NOT NULL DEFAULT '';

-- Per-subtask difficulty rating drives Model vs ModelHeavy selection.
ALTER TABLE plan_tasks ADD COLUMN IF NOT EXISTS difficulty TEXT NOT NULL DEFAULT '';
