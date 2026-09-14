CREATE TABLE IF NOT EXISTS mcp_servers (
    id TEXT PRIMARY KEY,
    enabled BOOLEAN NOT NULL DEFAULT false,
    transport TEXT NOT NULL DEFAULT 'stdio',
    command TEXT NOT NULL DEFAULT '',
    args JSONB NOT NULL DEFAULT '[]',
    env JSONB NOT NULL DEFAULT '{}',
    url TEXT NOT NULL DEFAULT '',
    headers JSONB NOT NULL DEFAULT '{}',
    allowed_tools JSONB NOT NULL DEFAULT '[]',
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
