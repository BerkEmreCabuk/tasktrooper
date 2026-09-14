-- Prompt caching was invisible to billing: a cached prefix and a fresh one
-- recorded the same prompt_tokens and were charged the same, so the whole
-- point of the cache (reads at ~10% of the input rate) never reached usd_spent.
-- Split the cached share out of the recorded prompt tokens and give each model
-- its own cache rates.
--
-- prompt_tokens stays the TOTAL, matching domain.Usage: the two new columns are
-- subsets of it, not additions to it. Billing subtracts them before applying
-- the base rate.
ALTER TABLE llm_usage
    ADD COLUMN IF NOT EXISTS cache_read_tokens  BIGINT NOT NULL DEFAULT 0,
    ADD COLUMN IF NOT EXISTS cache_write_tokens BIGINT NOT NULL DEFAULT 0;

-- Nullable on purpose. NULL means "this model has no separate cache rate",
-- which billing resolves to the FULL prompt price — an unpriced cache column
-- must never quietly hand out a discount. A zero would do exactly that.
ALTER TABLE model_prices
    ADD COLUMN IF NOT EXISTS usd_per_1m_cache_read  NUMERIC,
    ADD COLUMN IF NOT EXISTS usd_per_1m_cache_write NUMERIC;

-- Anthropic's published multipliers: a cache read costs 0.1x the input rate,
-- a 5-minute cache write 1.25x. Seeded off each row's own prompt price so the
-- ratio survives an operator repricing the base rate. Only the Claude rows
-- migrations/054 seeded are touched; anything an operator added by hand keeps
-- NULL and stays at full price until they say otherwise.
UPDATE model_prices
SET usd_per_1m_cache_read  = 0.10 * usd_per_1m_prompt,
    usd_per_1m_cache_write = 1.25 * usd_per_1m_prompt,
    updated_at             = now()
WHERE model IN (
        'claude-fable-5',
        'claude-opus-5',
        'claude-opus-4-8',
        'claude-opus-4-7',
        'claude-opus-4-6',
        'claude-sonnet-5',
        'claude-sonnet-4-6',
        'claude-haiku-4-5'
    )
  AND usd_per_1m_cache_read IS NULL
  AND usd_per_1m_cache_write IS NULL;
