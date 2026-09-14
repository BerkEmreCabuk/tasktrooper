ALTER TABLE llm_provider_configs
    ADD COLUMN IF NOT EXISTS timeout_seconds INTEGER NOT NULL DEFAULT 0;
