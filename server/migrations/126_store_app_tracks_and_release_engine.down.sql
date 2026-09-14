ALTER TABLE repositories
    DROP COLUMN IF EXISTS release_engine;

ALTER TABLE mobile_store_apps
    DROP COLUMN IF EXISTS app_name,
    DROP COLUMN IF EXISTS tracks,
    DROP COLUMN IF EXISTS tracks_synced_at;
