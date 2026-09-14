ALTER TABLE task_acceptance_criteria
    DROP COLUMN IF EXISTS canceled,
    DROP COLUMN IF EXISTS cancel_reason;
