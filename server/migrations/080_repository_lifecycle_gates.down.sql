-- Written for symmetry with the rest of migrations/; nothing executes .down.sql
-- (migrations/embed.go embeds only *.up.sql and there is no rollback path).
ALTER TABLE repositories DROP COLUMN IF EXISTS require_release_deploy;
ALTER TABLE repositories DROP COLUMN IF EXISTS require_review_chain;
