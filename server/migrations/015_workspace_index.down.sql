ALTER TABLE sessions DROP COLUMN IF EXISTS project_root;
DROP TABLE IF EXISTS workspace_file_hashes;
DROP TABLE IF EXISTS workspace_edges;
DROP TABLE IF EXISTS workspace_chunks;
DROP TABLE IF EXISTS workspace_symbols;
DROP TABLE IF EXISTS workspace_indexes;
