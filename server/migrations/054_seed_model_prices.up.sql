-- model_prices shipped empty, so every usage row joined to a zero price: usd_spent
-- stayed 0, the budget gate never tripped, and the usage screen reported "0 tokens
-- used" after a full session. Seed the Claude line at list price so a fresh install
-- accounts for spend out of the box.
--
-- Rates are USD per 1M tokens. Other providers are left to the admin UI rather than
-- guessed at here; an unpriced model still records tokens, it just contributes 0 USD.

INSERT INTO model_prices (model, usd_per_1m_prompt, usd_per_1m_completion) VALUES
    ('claude-fable-5',    10.0, 50.0),
    ('claude-opus-5',      5.0, 25.0),
    ('claude-opus-4-8',    5.0, 25.0),
    ('claude-opus-4-7',    5.0, 25.0),
    ('claude-opus-4-6',    5.0, 25.0),
    ('claude-sonnet-5',    3.0, 15.0),
    ('claude-sonnet-4-6',  3.0, 15.0),
    ('claude-haiku-4-5',   1.0,  5.0)
ON CONFLICT (model) DO NOTHING;
