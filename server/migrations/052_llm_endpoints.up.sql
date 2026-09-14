-- Named, multi-instance OpenAI-compatible LLM endpoints. Each row is a distinct
-- endpoint (own IP / Ollama / OpenRouter / Groq / vLLM / LM Studio) the user
-- names and configures. An endpoint id (uuid) is a valid provider ref: it can be
-- stored in active_llm_provider, embedding_llm_provider, agents.provider_type and
-- reuses the existing string-keyed client dispatch. Gemini + Anthropic stay as
-- fixed native providers (different SDK/wire format) in llm_provider_configs.

CREATE TABLE IF NOT EXISTS llm_endpoints (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    name            TEXT NOT NULL UNIQUE,
    base_url        TEXT NOT NULL,
    default_model   TEXT NOT NULL DEFAULT '',
    timeout_seconds INT  NOT NULL DEFAULT 120,
    configured      BOOLEAN NOT NULL DEFAULT true,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS llm_endpoint_secrets (
    endpoint_id       UUID PRIMARY KEY REFERENCES llm_endpoints(id) ON DELETE CASCADE,
    api_key_encrypted BYTEA,
    updated_at        TIMESTAMPTZ NOT NULL DEFAULT now()
);
