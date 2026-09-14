CREATE TABLE agent_performance_scores (
    id           UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    agent_id     UUID NOT NULL REFERENCES agents(id) ON DELETE CASCADE,
    score        NUMERIC(8,2) NOT NULL DEFAULT 100.0,
    runs_total   INT NOT NULL DEFAULT 0,
    runs_passed  INT NOT NULL DEFAULT 0,
    runs_revised INT NOT NULL DEFAULT 0,
    updated_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE(agent_id)
);

CREATE TABLE agent_score_events (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    agent_id    UUID NOT NULL REFERENCES agents(id) ON DELETE CASCADE,
    task_id     UUID REFERENCES board_tasks(id) ON DELETE SET NULL,
    event_type  TEXT NOT NULL,
    delta       NUMERIC(8,2) NOT NULL,
    score_after NUMERIC(8,2) NOT NULL,
    reason      TEXT,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_agent_score_events_agent ON agent_score_events(agent_id, created_at DESC);

ALTER TABLE team_agent_column_subscriptions
    ADD COLUMN IF NOT EXISTS task_type_filter TEXT[] DEFAULT NULL;
