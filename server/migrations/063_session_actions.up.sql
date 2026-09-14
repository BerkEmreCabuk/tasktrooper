-- Durable ledger of board-affecting actions an agent took inside a chat
-- session. Tool call traces only ever existed inside a single agent loop, so a
-- later turn had no id to act on and re-created records it had already made.
CREATE TABLE IF NOT EXISTS session_actions (
    id            UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    session_id    UUID NOT NULL REFERENCES sessions(id) ON DELETE CASCADE,
    run_id        UUID,
    agent_id      UUID,
    tool_name     TEXT NOT NULL,
    verb          TEXT NOT NULL,
    entity_kind   TEXT NOT NULL,
    entity_id     UUID,
    entity_key    TEXT NOT NULL DEFAULT '',
    title         TEXT NOT NULL DEFAULT '',
    task_column   TEXT NOT NULL DEFAULT '',
    priority      TEXT NOT NULL DEFAULT '',
    repository_id UUID,
    payload       JSONB,
    is_error      BOOLEAN NOT NULL DEFAULT false,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_session_actions_session_id
    ON session_actions(session_id, created_at);
