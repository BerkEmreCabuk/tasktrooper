-- What produced each index, so a model or dimension change can be detected
-- instead of silently mixing incomparable vectors.
--
-- workspace_chunks.embedding is untyped JSONB (015): nothing at the storage
-- layer stops a chunk embedded with one model from sitting next to one
-- embedded with another, and cosine similarity between them is not an error —
-- it is a confident, wrong ranking. These two columns record what the index
-- itself was built with; internal/domain.EmbeddingProvenanceStale is the
-- comparison against a tenant's CURRENT configuration.
--
-- Deliberately not a stored "stale" boolean: staleness depends on what the
-- tenant is configured for NOW, which moves independently of when any given
-- index was last built (a tenant can change embedding_llm_provider without
-- touching a single index row), so a persisted flag would drift the moment
-- settings changed, until some sweep caught up. Computing it at read time from
-- these two columns plus the tenant's live setting cannot drift.
ALTER TABLE workspace_indexes ADD COLUMN IF NOT EXISTS embedding_model TEXT NOT NULL DEFAULT '';
ALTER TABLE workspace_indexes ADD COLUMN IF NOT EXISTS embedding_dims INT NOT NULL DEFAULT 0;
