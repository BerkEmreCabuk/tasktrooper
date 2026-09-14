DROP INDEX IF EXISTS idx_workspace_chunks_content_trgm;
ALTER TABLE file_chunks DROP COLUMN IF EXISTS embedding_vec;
ALTER TABLE agent_memories DROP COLUMN IF EXISTS embedding_vec;
ALTER TABLE skills DROP COLUMN IF EXISTS embedding_vec;
ALTER TABLE workspace_chunks DROP COLUMN IF EXISTS embedding_vec;
