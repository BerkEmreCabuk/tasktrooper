CREATE TABLE IF NOT EXISTS agent_reflections (
    id                   UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    agent_id             UUID NOT NULL REFERENCES agents(id) ON DELETE CASCADE,
    team_id              UUID NOT NULL REFERENCES teams(id) ON DELETE CASCADE,
    trigger_kind         TEXT NOT NULL DEFAULT 'periodic',
    status               TEXT NOT NULL DEFAULT 'running',
    window_start         TIMESTAMPTZ NOT NULL,
    window_end           TIMESTAMPTZ NOT NULL,
    summary              TEXT NOT NULL DEFAULT '',
    performance_snapshot JSONB,
    raw_output           TEXT,
    error                TEXT,
    created_at           TIMESTAMPTZ NOT NULL DEFAULT now(),
    completed_at         TIMESTAMPTZ
);

CREATE INDEX IF NOT EXISTS idx_agent_reflections_agent_team
    ON agent_reflections(agent_id, team_id, created_at DESC);

CREATE TABLE IF NOT EXISTS agent_evolution_events (
    id                  UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    reflection_id       UUID REFERENCES agent_reflections(id) ON DELETE SET NULL,
    agent_id            UUID NOT NULL REFERENCES agents(id) ON DELETE CASCADE,
    team_id             UUID NOT NULL REFERENCES teams(id) ON DELETE CASCADE,
    change_type         TEXT NOT NULL,
    target_kind         TEXT NOT NULL,
    target_id           UUID,
    target_name         TEXT NOT NULL DEFAULT '',
    before_state        JSONB,
    after_state         JSONB,
    reverted_event_id   UUID REFERENCES agent_evolution_events(id) ON DELETE SET NULL,
    score_at_change     NUMERIC(8,2) NOT NULL DEFAULT 100.0,
    impact              TEXT NOT NULL DEFAULT 'pending',
    impact_evaluated_at TIMESTAMPTZ,
    created_at          TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_agent_evolution_events_agent_team
    ON agent_evolution_events(agent_id, team_id, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_agent_evolution_events_pending
    ON agent_evolution_events(impact, created_at) WHERE impact = 'pending';
