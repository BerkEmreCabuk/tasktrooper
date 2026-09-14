ALTER TABLE repositories
    DROP COLUMN IF EXISTS webhook_secret_enc,
    DROP COLUMN IF EXISTS webhook_hook_id;
