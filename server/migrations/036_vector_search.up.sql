-- pgvector + pg_trgm are optional accelerators: when the extensions are not
-- installed the DO blocks log a notice and the app falls back to in-Go cosine.
DO $$
BEGIN
    CREATE EXTENSION IF NOT EXISTS vector;
EXCEPTION WHEN OTHERS THEN
    RAISE NOTICE 'pgvector extension unavailable, vector search disabled: %', SQLERRM;
END $$;

DO $$
BEGIN
    CREATE EXTENSION IF NOT EXISTS pg_trgm;
EXCEPTION WHEN OTHERS THEN
    RAISE NOTICE 'pg_trgm extension unavailable, trigram search disabled: %', SQLERRM;
END $$;

DO $$
BEGIN
    IF EXISTS (SELECT 1 FROM pg_extension WHERE extname = 'vector') THEN
        EXECUTE 'ALTER TABLE workspace_chunks ADD COLUMN IF NOT EXISTS embedding_vec vector';
        EXECUTE 'ALTER TABLE skills ADD COLUMN IF NOT EXISTS embedding_vec vector';
        EXECUTE 'ALTER TABLE agent_memories ADD COLUMN IF NOT EXISTS embedding_vec vector';
        EXECUTE 'ALTER TABLE file_chunks ADD COLUMN IF NOT EXISTS embedding_vec vector';
    END IF;
END $$;

DO $$
BEGIN
    IF EXISTS (SELECT 1 FROM pg_extension WHERE extname = 'pg_trgm') THEN
        EXECUTE 'CREATE INDEX IF NOT EXISTS idx_workspace_chunks_content_trgm ON workspace_chunks USING gin (content gin_trgm_ops)';
    END IF;
END $$;
