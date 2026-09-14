DROP INDEX IF EXISTS idx_workspace_edges_index_from;
DROP INDEX IF EXISTS idx_workspace_symbols_index_file;
DROP INDEX IF EXISTS idx_workspace_chunks_index_file;
DROP INDEX IF EXISTS idx_workspace_indexes_repo_branch;

-- Branch rows violate the one-index-per-repository invariant being restored.
DELETE FROM workspace_indexes WHERE branch <> '';

CREATE UNIQUE INDEX IF NOT EXISTS idx_workspace_indexes_project
    ON workspace_indexes(repository_id) WHERE repository_id IS NOT NULL;

ALTER TABLE workspace_indexes DROP COLUMN IF EXISTS commit_sha;
ALTER TABLE workspace_indexes DROP COLUMN IF EXISTS branch;
