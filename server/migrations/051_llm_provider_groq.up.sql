-- Drop the CLI-backed providers (Claude Code / Cursor) — they were desktop-only
-- and no longer exist in the codebase — and add Groq (OpenAI-compatible) as a
-- first-class provider. Any active/embedding selection still pointing at a
-- removed CLI type is reset to 'local' so the app keeps booting.

UPDATE app_settings SET value = 'local'
    WHERE key = 'active_llm_provider' AND value IN ('claude_code_cli', 'cursor_cli');
UPDATE app_settings SET value = ''
    WHERE key = 'embedding_llm_provider' AND value IN ('claude_code_cli', 'cursor_cli');

-- Agents/templates pinned to a removed CLI provider fall back to the session
-- default (empty provider_type).
UPDATE agents SET provider_type = '' WHERE provider_type IN ('claude_code_cli', 'cursor_cli');
UPDATE agent_templates SET provider_type = '' WHERE provider_type IN ('claude_code_cli', 'cursor_cli');

DELETE FROM llm_provider_configs WHERE provider_type IN ('claude_code_cli', 'cursor_cli');

ALTER TABLE llm_provider_configs
    DROP CONSTRAINT IF EXISTS llm_provider_type_check;
ALTER TABLE llm_provider_configs
    ADD CONSTRAINT llm_provider_type_check
        CHECK (provider_type IN ('local', 'openai', 'groq', 'gemini', 'anthropic'));

INSERT INTO llm_provider_configs (provider_type, base_url, default_model, configured)
VALUES ('groq', 'https://api.groq.com/openai/v1', 'llama-3.3-70b-versatile', false)
ON CONFLICT (provider_type) DO NOTHING;
