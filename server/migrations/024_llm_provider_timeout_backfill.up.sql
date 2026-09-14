UPDATE llm_provider_configs
SET timeout_seconds = 300
WHERE provider_type = 'local' AND configured = true AND timeout_seconds = 0;

UPDATE llm_provider_configs
SET timeout_seconds = 120
WHERE provider_type IN ('openai', 'gemini', 'anthropic') AND configured = true AND timeout_seconds = 0;
