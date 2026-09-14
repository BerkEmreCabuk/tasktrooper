-- Reverse 055: put the anthropic provider back on the OpenRouter seed values.

UPDATE llm_provider_configs
SET base_url   = 'https://openrouter.ai/api/v1',
    updated_at = now()
WHERE provider_type = 'anthropic'
  AND base_url = 'https://api.anthropic.com/v1';

UPDATE llm_provider_configs
SET default_model = 'anthropic/claude-sonnet-4',
    updated_at    = now()
WHERE provider_type = 'anthropic'
  AND default_model = 'claude-sonnet-4-6';

UPDATE agents SET model = 'anthropic/claude-sonnet-4'
WHERE provider_type = 'anthropic' AND model = 'claude-sonnet-4-6';
UPDATE agents SET model_heavy = 'anthropic/claude-sonnet-4'
WHERE provider_type = 'anthropic' AND model_heavy = 'claude-sonnet-4-6';
