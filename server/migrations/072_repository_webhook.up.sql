-- Per-repository GitHub push-webhook state. The secret is stored encrypted
-- (AES-GCM via the same cipher that protects the GitHub token) because the
-- webhook endpoint is unauthenticated by definition: the HMAC signature over
-- this secret is the only thing standing between "GitHub pushed" and "anyone
-- on the internet can trigger a paid reindex". webhook_hook_id is the hook's
-- id on GitHub; 0 means no webhook has been installed yet, which the UI
-- surfaces as a warning with a one-click setup action.
ALTER TABLE repositories
    ADD COLUMN IF NOT EXISTS webhook_secret_enc TEXT NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS webhook_hook_id BIGINT NOT NULL DEFAULT 0;
