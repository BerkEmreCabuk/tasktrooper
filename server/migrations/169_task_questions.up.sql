-- Open questions an analiz run records for the human instead of ask_user or
-- prose buried in the report's risks section. A blocking question parks the
-- task (blocked_resource = 'analysis_questions'); a non-blocking one rides
-- along into analiz_review with its recommended answer. See
-- task_document_annotations (migration 164) for the nearest analogue: that is
-- a passage-anchored review comment, this is a standalone question with its
-- own answer lifecycle.
--
--   open      — asked, not yet answered (or re-opened by clearing an answer)
--   answered  — the human saved a non-empty answer
--   withdrawn — the agent withdrew it; only the agent can, never the human
CREATE TABLE IF NOT EXISTS task_questions (
    id                  UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    task_id             UUID NOT NULL REFERENCES board_tasks(id) ON DELETE CASCADE,
    question_key        TEXT NOT NULL,
    prompt              TEXT NOT NULL,
    kind                TEXT NOT NULL CHECK (kind IN ('product', 'technical')),
    blocking            BOOLEAN NOT NULL,
    recommended_answer  TEXT NOT NULL DEFAULT '',
    status              TEXT NOT NULL DEFAULT 'open' CHECK (status IN ('open', 'answered', 'withdrawn')),
    answer              TEXT NOT NULL DEFAULT '',
    answered_at         TIMESTAMPTZ,
    submitted_at        TIMESTAMPTZ,
    created_at          TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at          TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (task_id, question_key)
);

CREATE INDEX IF NOT EXISTS idx_task_questions_task
    ON task_questions (task_id, created_at);

-- record_open_questions and list_open_questions, for every agent that writes
-- analyses — same guarded-UPDATE, keyed-on-the-analyst-role backfill
-- migration 164 used for list_document_annotations/resolve_document_annotations,
-- re-running never appends a duplicate. Anchored on add_task_document (every
-- analyst-role agent, architect or a developer made the analyst, already has
-- it) rather than on an agent name.
UPDATE agents a
SET tool_policy = jsonb_set(
        a.tool_policy,
        '{allow_tools}',
        COALESCE(a.tool_policy->'allow_tools', '[]'::jsonb) || '["record_open_questions"]'::jsonb
    )
WHERE COALESCE(a.tool_policy->'allow_tools', '[]'::jsonb) ? 'add_task_document'
  AND NOT COALESCE(a.tool_policy->'allow_tools', '[]'::jsonb) ? 'record_open_questions'
  AND EXISTS (
      SELECT 1 FROM agent_role_assignments ara
      JOIN roles r ON r.id = ara.role_id
      WHERE ara.agent_id = a.id AND r.key = 'analyst'
  );

UPDATE agents a
SET tool_policy = jsonb_set(
        a.tool_policy,
        '{allow_tools}',
        COALESCE(a.tool_policy->'allow_tools', '[]'::jsonb) || '["list_open_questions"]'::jsonb
    )
WHERE COALESCE(a.tool_policy->'allow_tools', '[]'::jsonb) ? 'add_task_document'
  AND NOT COALESCE(a.tool_policy->'allow_tools', '[]'::jsonb) ? 'list_open_questions'
  AND EXISTS (
      SELECT 1 FROM agent_role_assignments ara
      JOIN roles r ON r.id = ara.role_id
      WHERE ara.agent_id = a.id AND r.key = 'analyst'
  );

-- The analyst role's own required_tools, so a FUTURE assignment of a new
-- developer to analyst (the "make them the analyst" flow in
-- application/workflow/admin.go's SetRoleAssignmentsChecked) is checked and
-- granted these two tools exactly like add_task_document already is.
UPDATE roles
SET required_tools = required_tools || ARRAY['record_open_questions', 'list_open_questions']
WHERE key = 'analyst'
  AND NOT required_tools @> ARRAY['record_open_questions'];
