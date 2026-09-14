CREATE TABLE IF NOT EXISTS workspace_indexes (
    id            UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    session_id    UUID NOT NULL REFERENCES sessions(id) ON DELETE CASCADE,
    root_path     TEXT NOT NULL,
    status        TEXT NOT NULL DEFAULT 'pending',
    file_count    INT NOT NULL DEFAULT 0,
    chunk_count   INT NOT NULL DEFAULT 0,
    symbol_count  INT NOT NULL DEFAULT 0,
    tree_text     TEXT NOT NULL DEFAULT '',
    indexed_at    TIMESTAMPTZ,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at    TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE UNIQUE INDEX IF NOT EXISTS idx_workspace_indexes_session ON workspace_indexes(session_id);

CREATE TABLE IF NOT EXISTS workspace_symbols (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    index_id    UUID NOT NULL REFERENCES workspace_indexes(id) ON DELETE CASCADE,
    file_path   TEXT NOT NULL,
    kind        TEXT NOT NULL,
    name        TEXT NOT NULL,
    signature   TEXT NOT NULL DEFAULT '',
    doc         TEXT NOT NULL DEFAULT '',
    start_line  INT NOT NULL DEFAULT 0,
    end_line    INT NOT NULL DEFAULT 0
);

CREATE INDEX IF NOT EXISTS idx_workspace_symbols_index ON workspace_symbols(index_id);
CREATE INDEX IF NOT EXISTS idx_workspace_symbols_name ON workspace_symbols(index_id, name);

CREATE TABLE IF NOT EXISTS workspace_chunks (
    id           UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    index_id     UUID NOT NULL REFERENCES workspace_indexes(id) ON DELETE CASCADE,
    file_path    TEXT NOT NULL,
    symbol_name  TEXT NOT NULL DEFAULT '',
    kind         TEXT NOT NULL DEFAULT '',
    start_line   INT NOT NULL DEFAULT 0,
    end_line     INT NOT NULL DEFAULT 0,
    language     TEXT NOT NULL DEFAULT '',
    signature    TEXT NOT NULL DEFAULT '',
    content      TEXT NOT NULL,
    embedding    JSONB NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_workspace_chunks_index ON workspace_chunks(index_id);

CREATE TABLE IF NOT EXISTS workspace_edges (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    index_id    UUID NOT NULL REFERENCES workspace_indexes(id) ON DELETE CASCADE,
    from_file   TEXT NOT NULL,
    from_symbol TEXT NOT NULL DEFAULT '',
    to_file     TEXT NOT NULL,
    to_symbol   TEXT NOT NULL DEFAULT '',
    edge_kind   TEXT NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_workspace_edges_index ON workspace_edges(index_id);

CREATE TABLE IF NOT EXISTS workspace_file_hashes (
    index_id    UUID NOT NULL REFERENCES workspace_indexes(id) ON DELETE CASCADE,
    file_path   TEXT NOT NULL,
    content_hash TEXT NOT NULL,
    PRIMARY KEY (index_id, file_path)
);

ALTER TABLE sessions ADD COLUMN IF NOT EXISTS project_root TEXT NOT NULL DEFAULT '';
