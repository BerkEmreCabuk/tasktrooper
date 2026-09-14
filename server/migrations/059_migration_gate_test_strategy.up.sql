-- Three gaps this closes:
--
-- 1. A schema change could reach production without ever having been applied
--    anywhere: the QA gate reads build/test results, not "did the migration
--    actually run". has_migration is detected from the changed files (never
--    from an agent's claim) and stage_verified_at records that a stage deploy
--    really applied it, which the release path now requires.
-- 2. Deploy targets knew the health URL but not the environment's public
--    address, so "where does this API live" was nowhere in the system.
-- 3. Not every repository wants a staging deploy per task; how a repo is
--    verified is now its own setting instead of a hard-coded flow.

ALTER TABLE board_tasks
    ADD COLUMN IF NOT EXISTS has_migration BOOLEAN NOT NULL DEFAULT false;
ALTER TABLE board_tasks
    ADD COLUMN IF NOT EXISTS stage_verified_at TIMESTAMPTZ;

-- local | stage | per_step
ALTER TABLE repositories
    ADD COLUMN IF NOT EXISTS test_strategy TEXT NOT NULL DEFAULT 'stage';

ALTER TABLE repository_deploy_targets
    ADD COLUMN IF NOT EXISTS base_url TEXT NOT NULL DEFAULT '';

-- The release gate asks "does this task carry a migration that stage has not
-- verified", per repository.
CREATE INDEX IF NOT EXISTS idx_board_tasks_migration_gate
    ON board_tasks (repository_id) WHERE has_migration AND stage_verified_at IS NULL;
