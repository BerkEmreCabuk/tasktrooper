-- Per-role verification verdicts on acceptance criteria. The criterion's
-- `completed` flag is the implementer's claim that the work was done; QA and
-- PM each record their own approve/reject per criterion while testing, with a
-- note explaining any rejection. One row per (criterion, role): a re-test
-- overwrites the previous verdict instead of stacking history — the task's
-- comments carry the narrative.
CREATE TABLE IF NOT EXISTS task_criterion_checks (
    id           UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    criterion_id UUID NOT NULL REFERENCES task_acceptance_criteria(id) ON DELETE CASCADE,
    role         TEXT NOT NULL CHECK (role IN ('qa', 'pm')),
    agent_id     UUID REFERENCES agents(id) ON DELETE SET NULL,
    approved     BOOLEAN NOT NULL,
    note         TEXT NOT NULL DEFAULT '',
    checked_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (criterion_id, role)
);

-- Tool policies are written on agent CREATE only (admin customizations must
-- survive restarts), so existing installs need the new review_criterion tool
-- backfilled into the QA and PM policies — same pattern as migration 070's
-- subscription backfill.
UPDATE agents
SET tool_policy = jsonb_set(
        tool_policy,
        '{allow_tools}',
        COALESCE(tool_policy->'allow_tools', '[]'::jsonb) || '["review_criterion"]'::jsonb
    )
WHERE name IN ('qa-agent', 'product-manager')
  AND NOT COALESCE(tool_policy->'allow_tools', '[]'::jsonb) ? 'review_criterion';
