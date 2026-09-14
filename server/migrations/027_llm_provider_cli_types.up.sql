ALTER TABLE llm_provider_configs
    DROP CONSTRAINT IF EXISTS llm_provider_type_check;

ALTER TABLE llm_provider_configs
    ADD CONSTRAINT llm_provider_type_check
        CHECK (provider_type IN ('local', 'openai', 'gemini', 'anthropic', 'claude_code_cli', 'cursor_cli'));

INSERT INTO llm_provider_configs (provider_type, base_url, default_model, configured)
VALUES
    ('claude_code_cli', 'claude', 'claude-sonnet-4-6', false),
    ('cursor_cli',      'http://localhost:4141/v1', 'cursor-small', false)
ON CONFLICT (provider_type) DO NOTHING;
