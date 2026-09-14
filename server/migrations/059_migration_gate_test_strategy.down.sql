DROP INDEX IF EXISTS idx_board_tasks_migration_gate;
ALTER TABLE repository_deploy_targets DROP COLUMN IF EXISTS base_url;
ALTER TABLE repositories DROP COLUMN IF EXISTS test_strategy;
ALTER TABLE board_tasks DROP COLUMN IF EXISTS stage_verified_at;
ALTER TABLE board_tasks DROP COLUMN IF EXISTS has_migration;
