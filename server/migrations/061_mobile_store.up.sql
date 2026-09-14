CREATE TABLE store_credentials (
    provider   TEXT PRIMARY KEY CHECK (provider IN ('asc', 'google_play')),
    data       BYTEA NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE mobile_store_apps (
    id                     UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    repository_id          UUID NOT NULL REFERENCES repositories(id) ON DELETE CASCADE,
    platform               TEXT NOT NULL CHECK (platform IN ('ios', 'android')),
    identifier             TEXT NOT NULL,
    store_app_id           TEXT NOT NULL DEFAULT '',
    state                  TEXT NOT NULL DEFAULT 'unregistered'
        CHECK (state IN ('unregistered', 'onboarding', 'test_ready', 'live')),
    review_state           TEXT NOT NULL DEFAULT '',
    last_submitted_version TEXT NOT NULL DEFAULT '',
    last_released_version  TEXT NOT NULL DEFAULT '',
    checklist              JSONB NOT NULL DEFAULT '[]',
    onboarding_task_id     UUID,
    first_published_at     TIMESTAMPTZ,
    created_at             TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at             TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (repository_id, platform)
);

CREATE TABLE signing_assets (
    id         UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    kind       TEXT NOT NULL CHECK (kind IN ('dist_cert', 'profile', 'upload_keystore')),
    identifier TEXT NOT NULL DEFAULT '',
    serial     TEXT NOT NULL DEFAULT '',
    data       BYTEA NOT NULL,
    expires_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (kind, identifier)
);
