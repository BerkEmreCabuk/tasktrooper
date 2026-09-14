CREATE TABLE IF NOT EXISTS audit_logs (
    id             UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    request_id     TEXT NOT NULL,
    api_key_name   TEXT,
    tool_name      TEXT NOT NULL,
    arguments      TEXT,
    result_preview TEXT,
    duration_ms    BIGINT NOT NULL DEFAULT 0,
    is_error       BOOLEAN NOT NULL DEFAULT false,
    created_at     TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_audit_logs_created_at ON audit_logs(created_at DESC);
CREATE INDEX IF NOT EXISTS idx_audit_logs_request_id ON audit_logs(request_id);
