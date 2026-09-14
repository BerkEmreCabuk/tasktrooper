-- iOS Live Activity push token'ları. kind='start' cihaz başına push-to-start
-- token'ıdır (task bağımsız); kind='update' çalışan bir aktivitenin task'a
-- bağlı güncelleme token'ıdır.
CREATE TABLE IF NOT EXISTS live_activity_tokens (
    id         UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    kind       TEXT NOT NULL CHECK (kind IN ('start', 'update')),
    token      TEXT NOT NULL UNIQUE,
    task_id    UUID,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS idx_live_activity_task ON live_activity_tokens (task_id) WHERE task_id IS NOT NULL;
