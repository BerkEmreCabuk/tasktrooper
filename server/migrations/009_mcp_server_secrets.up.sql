CREATE TABLE IF NOT EXISTS mcp_server_secrets (
    server_id TEXT NOT NULL REFERENCES mcp_servers(id) ON DELETE CASCADE,
    location TEXT NOT NULL DEFAULT 'env',
    key TEXT NOT NULL,
    encrypted_value BYTEA NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (server_id, location, key)
);
