-- HTML analysis reports, and a human's passage-level review of them.
--
-- An analiz task's deliverable becomes ONE self-contained HTML page (spec and
-- plan are sections of it) instead of two markdown documents. The page is
-- sanitized on save by the application; the column only records which of the
-- two renderings a document is. Every existing row is markdown, which is what
-- the default gives it.
--
-- The review happens on that page: the human selects a passage, writes a
-- comment, and submits the whole batch at once, which moves the task to
-- need_revision. Each comment is a text-quote anchor (quote + a little prefix
-- and suffix of the rendered text) rather than an offset, because the
-- architect rewrites the document in answer and an offset would point at the
-- wrong words afterwards.
--
--   open      — written, not yet sent; the human may still edit or delete it
--   submitted — sent with a review; the revision run is told about it
--   resolved  — the agent answered it (reply) while revising
ALTER TABLE task_documents
    ADD COLUMN IF NOT EXISTS format TEXT NOT NULL DEFAULT 'markdown';

ALTER TABLE task_documents DROP CONSTRAINT IF EXISTS task_documents_format_check;
ALTER TABLE task_documents ADD CONSTRAINT task_documents_format_check
    CHECK (format IN ('markdown', 'html'));

CREATE TABLE IF NOT EXISTS task_document_annotations (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    task_id         UUID NOT NULL REFERENCES board_tasks(id) ON DELETE CASCADE,
    document_id     UUID NOT NULL REFERENCES task_documents(id) ON DELETE CASCADE,
    quote           TEXT NOT NULL,
    prefix          TEXT NOT NULL DEFAULT '',
    suffix          TEXT NOT NULL DEFAULT '',
    body            TEXT NOT NULL,
    status          TEXT NOT NULL DEFAULT 'open' CHECK (status IN ('open', 'submitted', 'resolved')),
    reply           TEXT NOT NULL DEFAULT '',
    created_by_type TEXT NOT NULL DEFAULT 'user' CHECK (created_by_type IN ('user', 'agent')),
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    submitted_at    TIMESTAMPTZ,
    resolved_at     TIMESTAMPTZ
);

CREATE INDEX IF NOT EXISTS idx_task_document_annotations_task
    ON task_document_annotations (task_id, created_at);
CREATE INDEX IF NOT EXISTS idx_task_document_annotations_document
    ON task_document_annotations (document_id);

-- list_document_annotations and resolve_document_annotations, for every agent
-- that writes analyses.
--
-- Tool policies are written on agent CREATE only (an admin's customization must
-- survive a restart), so existing installs need new tools backfilled here — the
-- same guarded-UPDATE pattern as migrations 106 and 108: re-running never
-- appends a duplicate.
--
-- Keyed on the analyst role AND update_task_document rather than on an agent
-- name: the agent that revises an analysis is whichever one the install
-- assigned analiz work to, and it is only useful to it if it can also rewrite
-- the document the comments are about. An empty allow list is unrestricted
-- and already has both.
UPDATE agents a
SET tool_policy = jsonb_set(
        a.tool_policy,
        '{allow_tools}',
        COALESCE(a.tool_policy->'allow_tools', '[]'::jsonb) || '["list_document_annotations"]'::jsonb
    )
WHERE COALESCE(a.tool_policy->'allow_tools', '[]'::jsonb) ? 'update_task_document'
  AND NOT COALESCE(a.tool_policy->'allow_tools', '[]'::jsonb) ? 'list_document_annotations'
  AND EXISTS (
      SELECT 1 FROM agent_role_assignments ara
      JOIN roles r ON r.id = ara.role_id
      WHERE ara.agent_id = a.id AND r.key = 'analyst'
  );

UPDATE agents a
SET tool_policy = jsonb_set(
        a.tool_policy,
        '{allow_tools}',
        COALESCE(a.tool_policy->'allow_tools', '[]'::jsonb) || '["resolve_document_annotations"]'::jsonb
    )
WHERE COALESCE(a.tool_policy->'allow_tools', '[]'::jsonb) ? 'update_task_document'
  AND NOT COALESCE(a.tool_policy->'allow_tools', '[]'::jsonb) ? 'resolve_document_annotations'
  AND EXISTS (
      SELECT 1 FROM agent_role_assignments ara
      JOIN roles r ON r.id = ara.role_id
      WHERE ara.agent_id = a.id AND r.key = 'analyst'
  );
