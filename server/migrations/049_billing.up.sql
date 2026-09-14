-- Per-tenant billing: effective plan snapshot + per-model credit structure +
-- pause bookkeeping for budget-exhausted tasks (auto-resumed on period roll).

CREATE TABLE IF NOT EXISTS billing_plan (
    id                   SMALLINT PRIMARY KEY DEFAULT 1 CHECK (id = 1),
    name                 TEXT NOT NULL DEFAULT 'free',
    usd_budget           DOUBLE PRECISION NOT NULL DEFAULT 0,   -- 0 = unlimited
    max_concurrent_tasks INT NOT NULL DEFAULT 3,
    period_days          INT NOT NULL DEFAULT 30,
    period_start         TIMESTAMPTZ NOT NULL DEFAULT now(),
    display_token_rate   DOUBLE PRECISION NOT NULL DEFAULT 0.000002, -- USD/token for token display
    updated_at           TIMESTAMPTZ NOT NULL DEFAULT now()
);
INSERT INTO billing_plan (id) VALUES (1) ON CONFLICT DO NOTHING;

-- Our internal credit structure: USD per 1M tokens per model.
CREATE TABLE IF NOT EXISTS model_prices (
    model                 TEXT PRIMARY KEY,
    usd_per_1m_prompt     DOUBLE PRECISION NOT NULL DEFAULT 0,
    usd_per_1m_completion DOUBLE PRECISION NOT NULL DEFAULT 0,
    updated_at            TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS quota_paused_tasks (
    task_id       UUID PRIMARY KEY,
    repository_id UUID NOT NULL,
    paused_at     TIMESTAMPTZ NOT NULL DEFAULT now()
);
