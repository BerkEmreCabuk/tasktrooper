-- The board a brand-new tenant starts with.
--
-- This is what migrations 010/018/041/042/049/051/054/056/093 used to INSERT
-- at migration time, when one database was one tenant. It runs inside a
-- transaction whose app.tenant_id is already the new tenant, so every row
-- picks its tenant_id up from the column default (migration 114) and none of
-- these statements has to mention it.
--
-- ON CONFLICT DO NOTHING throughout: two requests from the same brand-new
-- tenant can race here, and losing that race must be a no-op rather than a
-- failed first page load.
--
-- **New seed data belongs here, not in a migration.** A migration runs once
-- for the whole fleet; this runs once per tenant. Adding a board column in a
-- migration would give it to nobody.

-- The default column template. Order matters only through `position`; the
-- slugs are what board_tasks.board_column is validated against and what agent
-- subscriptions name, so they are the part that must not drift.
INSERT INTO board_columns (slug, label, position, is_backlog) VALUES
    ('backlog',       'Backlog',        0, true),
    ('todo',          'Todo',           1, false),
    ('in_progress',   'In Progress',    2, false),
    ('analiz_review', 'Analiz Review',  3, false),
    ('code_review',   'Code Review',    4, false),
    ('ready_for_qa',  'Ready for QA',   5, false),
    ('in_qa',         'In QA',          6, false),
    ('need_revision', 'Need Revision',  7, false),
    ('pm_uat',        'PM UAT',         8, false),
    ('human_uat',     'Human UAT',      9, false),
    ('blocked',       'Blocked',       10, false),
    ('done',          'Done',          11, false),
    ('released',      'Released',      12, false)
ON CONFLICT DO NOTHING;

-- One counter per task type. They exist up front so the first task of each
-- type takes number 1 rather than racing to create its own row.
INSERT INTO board_task_counters (task_type, last_number) VALUES
    ('task', 0), ('bug', 0), ('analiz', 0)
ON CONFLICT DO NOTHING;

-- The single-row tables. Their CHECK (id = 1) now means one row per tenant
-- (migration 114), so `id` is still literally 1 here.
INSERT INTO board_settings (id) VALUES (1) ON CONFLICT DO NOTHING;
INSERT INTO billing_plan (id) VALUES (1) ON CONFLICT DO NOTHING;

-- Settings a tenant needs a value for before anything can be configured.
-- workspace_root is a path on whichever host serves this tenant; the host-root
-- translation in the stores makes a foreign value harmless (see .ai/workspace.md).
INSERT INTO app_settings (key, value) VALUES
    ('workspace_root',      './data/workspaces'),
    ('default_language',    'tr'),
    ('active_llm_provider', 'local')
ON CONFLICT DO NOTHING;

-- The provider catalog, all unconfigured: the row is the offer, `configured`
-- is whether this tenant has supplied a key.
INSERT INTO llm_provider_configs (provider_type, base_url, default_model) VALUES
    ('local',     'http://127.0.0.1:1234/v1',                                  ''),
    ('openai',    'https://api.openai.com/v1',                                 'gpt-4o'),
    ('anthropic', 'https://api.anthropic.com/v1',                              'claude-sonnet-4-6'),
    ('gemini',    'https://generativelanguage.googleapis.com/v1beta/openai/',  'gemini-2.0-flash'),
    ('groq',      'https://api.groq.com/openai/v1',                            'llama-3.3-70b-versatile')
ON CONFLICT DO NOTHING;

-- Token prices, per tenant because the admin billing routes let a tenant edit
-- them. These are the defaults every tenant starts from.
INSERT INTO model_prices (model, usd_per_1m_prompt, usd_per_1m_completion, usd_per_1m_cache_read, usd_per_1m_cache_write) VALUES
    ('claude-opus-5',     5,  25, 0.5,  6.25),
    ('claude-sonnet-5',   3,  15, 0.3,  3.75),
    ('claude-fable-5',   10,  50, 1.0, 12.50),
    ('claude-opus-4-8',   5,  25, 0.5,  6.25),
    ('claude-opus-4-7',   5,  25, 0.5,  6.25),
    ('claude-opus-4-6',   5,  25, 0.5,  6.25),
    ('claude-sonnet-4-6', 3,  15, 0.3,  3.75),
    ('claude-haiku-4-5',  1,   5, 0.1,  1.25)
ON CONFLICT DO NOTHING;
