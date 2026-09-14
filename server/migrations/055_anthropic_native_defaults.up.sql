-- Migration 018 seeded the anthropic provider against OpenRouter
-- ('https://openrouter.ai/api/v1' + the OpenRouter slug 'anthropic/claude-sonnet-4'),
-- but the adapter talks the native Anthropic API. Connecting the provider only
-- stores the API key, so the stale base URL and slug survived: every agent on
-- the anthropic provider sent an OpenRouter model id to api.anthropic.com and
-- got a 404 back.
--
-- Repair only rows still carrying those exact seeded values, so an operator who
-- deliberately points the provider at OpenRouter (or any other gateway) keeps
-- their setting.

UPDATE llm_provider_configs
SET base_url   = 'https://api.anthropic.com/v1',
    updated_at = now()
WHERE provider_type = 'anthropic'
  AND base_url = 'https://openrouter.ai/api/v1';

UPDATE llm_provider_configs
SET default_model = 'claude-sonnet-4-6',
    updated_at    = now()
WHERE provider_type = 'anthropic'
  AND default_model = 'anthropic/claude-sonnet-4';

-- Agents pinned to the OpenRouter-style slug follow the same repair; an empty
-- model means "use the provider default".
UPDATE agents SET model = 'claude-sonnet-4-6'
WHERE provider_type = 'anthropic' AND model = 'anthropic/claude-sonnet-4';
UPDATE agents SET model_heavy = 'claude-sonnet-4-6'
WHERE provider_type = 'anthropic' AND model_heavy = 'anthropic/claude-sonnet-4';
