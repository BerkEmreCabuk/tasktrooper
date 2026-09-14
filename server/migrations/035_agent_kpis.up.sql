CREATE TABLE IF NOT EXISTS agent_kpis (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    agent_id    UUID NOT NULL REFERENCES agents(id) ON DELETE CASCADE,
    metric_key  TEXT NOT NULL,
    name        TEXT NOT NULL DEFAULT '',
    description TEXT NOT NULL DEFAULT '',
    period      TEXT NOT NULL DEFAULT 'weekly',
    target_full NUMERIC(12,2) NOT NULL,
    target_half NUMERIC(12,2) NOT NULL,
    weight      NUMERIC(8,2) NOT NULL DEFAULT 1.0,
    enabled     BOOLEAN NOT NULL DEFAULT true,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE(agent_id, metric_key, period)
);

CREATE TABLE IF NOT EXISTS agent_kpi_results (
    id             UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    kpi_id         UUID NOT NULL REFERENCES agent_kpis(id) ON DELETE CASCADE,
    agent_id       UUID NOT NULL REFERENCES agents(id) ON DELETE CASCADE,
    team_id        UUID NOT NULL REFERENCES teams(id) ON DELETE CASCADE,
    period_start   TIMESTAMPTZ NOT NULL,
    period_end     TIMESTAMPTZ NOT NULL,
    measured_value NUMERIC(12,2) NOT NULL DEFAULT 0,
    attainment     NUMERIC(4,2) NOT NULL DEFAULT 0,
    computed_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE(kpi_id, team_id, period_start)
);

CREATE INDEX IF NOT EXISTS idx_agent_kpi_results_agent_team
    ON agent_kpi_results(agent_id, team_id, period_start DESC);

ALTER TABLE agent_templates ADD COLUMN IF NOT EXISTS kpis JSONB NOT NULL DEFAULT '[]'::jsonb;
