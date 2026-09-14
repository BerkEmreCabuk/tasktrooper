-- Branch-aware workspace indexes: one row per (repository, branch) instead of
-- one per repository. branch = '' is the repository's default-branch index and
-- keeps the previous semantics for every existing row and caller.
ALTER TABLE workspace_indexes ADD COLUMN IF NOT EXISTS branch TEXT NOT NULL DEFAULT '';
ALTER TABLE workspace_indexes ADD COLUMN IF NOT EXISTS commit_sha TEXT NOT NULL DEFAULT '';

DROP INDEX IF EXISTS idx_workspace_indexes_project;
CREATE UNIQUE INDEX IF NOT EXISTS idx_workspace_indexes_repo_branch
    ON workspace_indexes(repository_id, branch) WHERE repository_id IS NOT NULL;

-- Incremental reindex deletes rows per file; without these the per-file
-- deletes scan every row of the index.
CREATE INDEX IF NOT EXISTS idx_workspace_chunks_index_file ON workspace_chunks(index_id, file_path);
CREATE INDEX IF NOT EXISTS idx_workspace_symbols_index_file ON workspace_symbols(index_id, file_path);
CREATE INDEX IF NOT EXISTS idx_workspace_edges_index_from ON workspace_edges(index_id, from_file);
