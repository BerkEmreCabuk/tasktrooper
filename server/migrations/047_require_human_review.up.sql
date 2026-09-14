-- Per-repo toggle: require a human to approve at the review stage. When true,
-- the agent-reviewed columns (code_review, pm_uat) become human approval gates
-- (pipeline still runs, but no reviewing agent is dispatched).
ALTER TABLE repositories
    ADD COLUMN IF NOT EXISTS require_human_review BOOLEAN NOT NULL DEFAULT false;
