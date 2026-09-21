-- agent_column_instructions: per-agent, per-column default prompts the operator
-- can hand an agent for "what to do when a task arrives in this column". A
-- separate store from agent_column_subscriptions on purpose: a column an agent
-- is dispatched into need not be one it watches (a developer does not watch
-- need_revision, yet needs its instruction), and instructions must not change
-- column-watch dispatch behaviour. Keyed by column_slug like subscriptions;
-- no FK to board_columns, which ReplaceColumns deletes and reinserts.
CREATE TABLE agent_column_instructions (
    agent_id    UUID NOT NULL REFERENCES agents(id) ON DELETE CASCADE,
    column_slug TEXT NOT NULL,
    instruction TEXT NOT NULL DEFAULT '',
    PRIMARY KEY (agent_id, column_slug)
);