-- Her LLM çağrısının token kullanımı (maliyet takibi). Kayıt best-effort'tur:
-- yazılamayan satır çağrıyı etkilemez.
CREATE TABLE IF NOT EXISTS llm_usage (
    id                UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    model             TEXT NOT NULL,
    prompt_tokens     INT NOT NULL DEFAULT 0,
    completion_tokens INT NOT NULL DEFAULT 0,
    created_at        TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS idx_llm_usage_created ON llm_usage (created_at);
CREATE INDEX IF NOT EXISTS idx_llm_usage_model ON llm_usage (model, created_at);
