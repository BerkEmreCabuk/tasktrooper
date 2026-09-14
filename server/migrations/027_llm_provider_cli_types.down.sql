DELETE FROM llm_provider_configs WHERE provider_type IN ('claude_code_cli', 'cursor_cli');

ALTER TABLE llm_provider_configs
    DROP CONSTRAINT IF EXISTS llm_provider_type_check;

ALTER TABLE llm_provider_configs
    ADD CONSTRAINT llm_provider_type_check
        CHECK (provider_type IN ('local', 'openai', 'gemini', 'anthropic'));
