CREATE TABLE IF NOT EXISTS llm_provider_configs (
    provider_type TEXT PRIMARY KEY,
    base_url TEXT NOT NULL DEFAULT '',
    default_model TEXT NOT NULL DEFAULT '',
    configured BOOLEAN NOT NULL DEFAULT false,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT llm_provider_type_check CHECK (provider_type IN ('local', 'openai', 'gemini', 'anthropic'))
);

CREATE TABLE IF NOT EXISTS llm_provider_secrets (
    provider_type TEXT PRIMARY KEY REFERENCES llm_provider_configs(provider_type) ON DELETE CASCADE,
    api_key_encrypted BYTEA,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

INSERT INTO app_settings (key, value) VALUES ('active_llm_provider', 'local')
ON CONFLICT (key) DO NOTHING;

INSERT INTO llm_provider_configs (provider_type, base_url, default_model, configured)
VALUES
    ('local', 'http://127.0.0.1:1234/v1', '', false),
    ('openai', 'https://api.openai.com/v1', 'gpt-4o', false),
    ('gemini', 'https://generativelanguage.googleapis.com/v1beta/openai/', 'gemini-2.0-flash', false),
    ('anthropic', 'https://openrouter.ai/api/v1', 'anthropic/claude-sonnet-4', false)
ON CONFLICT (provider_type) DO NOTHING;
