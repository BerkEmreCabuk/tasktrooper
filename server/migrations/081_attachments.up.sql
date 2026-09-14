-- Binary attachments (images + documents) for board tasks and chat messages.
-- Bytes live in Postgres (BYTEA), NOT on the tenant pod's disk: pod storage is
-- ephemeral in the cloud deploy, and the existing files/RAG pipeline assumes
-- UTF-8 text — this is a parallel concept, not a replacement for it.

CREATE TABLE IF NOT EXISTS attachments (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    repository_id   UUID REFERENCES repositories(id) ON DELETE CASCADE,
    filename        TEXT NOT NULL,
    content_type    TEXT NOT NULL,
    size_bytes      BIGINT NOT NULL,
    sha256          TEXT NOT NULL,
    data            BYTEA NOT NULL,
    created_by_type TEXT NOT NULL DEFAULT 'user',
    created_by_id   TEXT,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS task_attachments (
    task_id       UUID NOT NULL REFERENCES board_tasks(id) ON DELETE CASCADE,
    attachment_id UUID NOT NULL REFERENCES attachments(id) ON DELETE CASCADE,
    position      INT NOT NULL DEFAULT 0,
    PRIMARY KEY (task_id, attachment_id)
);

CREATE TABLE IF NOT EXISTS session_message_attachments (
    message_id    UUID NOT NULL REFERENCES session_messages(id) ON DELETE CASCADE,
    attachment_id UUID NOT NULL REFERENCES attachments(id) ON DELETE CASCADE,
    PRIMARY KEY (message_id, attachment_id)
);

CREATE INDEX IF NOT EXISTS idx_task_attachments_attachment ON task_attachments(attachment_id);
CREATE INDEX IF NOT EXISTS idx_session_message_attachments_attachment ON session_message_attachments(attachment_id);

-- Tool policy backfill: `attach_task_file` joins roleBoardCreateTools
-- (create_board_task, add_task_document) for new installs; existing installs
-- get it appended here — same guarded, re-runnable pattern as migration 076.
UPDATE agents
SET tool_policy = jsonb_set(
        tool_policy,
        '{allow_tools}',
        COALESCE(tool_policy->'allow_tools', '[]'::jsonb) || '["attach_task_file"]'::jsonb
    )
WHERE name IN ('product-manager', 'system-architect')
  AND NOT COALESCE(tool_policy->'allow_tools', '[]'::jsonb) ? 'attach_task_file';
