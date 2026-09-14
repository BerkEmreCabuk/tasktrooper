-- Which local agent CLI this tenant has connected, and the evidence the
-- connect gathered.
--
-- "Connected" here is a different fact from a row in llm_provider_configs, and
-- the separate table is what keeps them from being confused. A provider config
-- means "dial this base URL with this key"; claude_code and cursor_agent have
-- neither, and llmprovider.Service deliberately refuses connect/test/activate
-- for them. What this row records is that somebody verified the binary exists
-- on the runner host and holds a session, and that every enabled agent's
-- catalog was written out in the shape that binary reads.
--
-- The singleton column is the mutual exclusion, in the schema rather than in
-- the service. At most one CLI is connected at a time: the two read different
-- catalog layouts from the same agent rows, so two connections mean two on-disk
-- copies drifting apart, and a settings page that can answer "which CLI runs my
-- board?" with "both" has not answered it. A BOOLEAN primary key with a CHECK
-- that it is true admits exactly one row, so a second connect can only be an
-- UPSERT onto the first — there is no ordering of statements, and no bug in a
-- future caller, that leaves both stored.
CREATE TABLE IF NOT EXISTS agent_cli_connection (
    singleton      BOOLEAN     PRIMARY KEY DEFAULT TRUE CHECK (singleton),
    flavor         TEXT        NOT NULL,
    provider_type  TEXT        NOT NULL,
    -- Evidence, not configuration: the executor resolves its own binary at boot
    -- and never reads these. They are here so the settings page can show what
    -- was verified and when.
    binary_path    TEXT        NOT NULL DEFAULT '',
    binary_version TEXT        NOT NULL DEFAULT '',
    -- Where the connect-time catalog snapshot was written. NOT the source a
    -- board run reads — a run materialises its own agent into its own task
    -- workspace, from the database, at dispatch.
    catalog_path   TEXT        NOT NULL DEFAULT '',
    agent_count    INTEGER     NOT NULL DEFAULT 0,
    skill_count    INTEGER     NOT NULL DEFAULT 0,
    connected_at   TIMESTAMPTZ NOT NULL DEFAULT now()
);
