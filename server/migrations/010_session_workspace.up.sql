ALTER TABLE sessions ADD COLUMN IF NOT EXISTS workspace_dir TEXT NOT NULL DEFAULT '';

CREATE TABLE IF NOT EXISTS app_settings (
    key         TEXT PRIMARY KEY,
    value       TEXT NOT NULL,
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);

INSERT INTO app_settings (key, value) VALUES ('workspace_root', './data/workspaces')
ON CONFLICT (key) DO NOTHING;

INSERT INTO app_settings (key, value) VALUES ('default_language', 'tr')
ON CONFLICT (key) DO NOTHING;
