-- Work order, and the analysis an implementation task came out of.
--
-- Two things the board could state but not act on:
--
--   * WHERE THE SPEC IS. An analiz task's deliverable is a set of documents
--     attached to that task (never a docs/ commit — an analysis produces no
--     diff). The implementation tasks the architect opens out of it had no way
--     to say which analysis they came from, so the agent picking one up was
--     handed a title, a description and no route to the spec that was written
--     for it. derived_from is that route: the implementation task is the
--     source, the analiz task is the target, and board.Runner reads the
--     target's documents into the run's context.
--
--   * WHO GOES FIRST. task_relations has modelled 'blocks' since migration 022
--     and almost nothing read it: repository.Service.validateMoveAllowed
--     refused a MOVE into todo/in_progress, which caught a human dragging a
--     card and nothing else. A task created straight into todo with an open
--     blocker, a reconciler sweep, a sweeper resume — every one of those
--     reached the dispatcher and started a run. The gate now sits in the
--     dispatcher itself and parks the card on the work_order resource instead.
--
-- Additive: a widened CHECK and one index. No existing row changes meaning.

-- Migration 084 widened this constraint once already, from ('blocks') to
-- ('blocks', 'deploy_depends_on'); the same drop-and-recreate widens it again.
-- Keeping the constraint is the point — it is what stops a typo'd relation_type
-- from becoming a relation no read path will ever match, which for a provenance
-- link means an implementation task that silently has no spec.
ALTER TABLE task_relations DROP CONSTRAINT IF EXISTS task_relations_relation_type_check;
ALTER TABLE task_relations ADD CONSTRAINT task_relations_relation_type_check
    CHECK (relation_type IN ('blocks', 'deploy_depends_on', 'derived_from'));

-- The blocker lookup runs from the DISPATCHER now, which means once per board
-- event on every task that reaches a working column — not once per release the
-- way the deploy-dependency read does. idx_task_relations_target (migration
-- 022) already covers target_task_id, but every one of these reads filters by
-- relation_type as well, and the blocks rows are a minority of the table.
--
-- The index is deliberately NOT partial on relation_type = 'blocks': the same
-- (target, type) shape serves the derived_from reverse lookup ("which tasks came
-- out of this analysis"), which the SPA will want as soon as it renders the
-- analiz card.
CREATE INDEX IF NOT EXISTS idx_task_relations_target_type
    ON task_relations (target_task_id, relation_type);

-- list_task_documents, for every role that can already read task comments.
--
-- Tool policies are written on agent CREATE only (an admin's customization must
-- survive a restart), so existing installs need new tools backfilled here — the
-- same guarded-UPDATE pattern as migrations 081, 085, 088, 104 and 105:
-- re-running never appends a duplicate.
--
-- Keyed on list_task_comments rather than on a role name, deliberately. The two
-- tools answer the same kind of question about the same card ("what has been
-- written on this task"), so any agent an operator trusted with one should have
-- the other; and an install that customized its roster still gets the right set
-- without this migration having to know what the roster is.
--
-- It matters most for developers, who are the ones handed an implementation
-- task derived from an analiz task. Their specification is now a DOCUMENT on
-- that analiz task and nothing else — no docs/ commit, no file in the branch —
-- so without this tool the run context is the only way they ever see it, and a
-- long plan cannot be re-read.
UPDATE agents
SET tool_policy = jsonb_set(
        tool_policy,
        '{allow_tools}',
        COALESCE(tool_policy->'allow_tools', '[]'::jsonb) || '["list_task_documents"]'::jsonb
    )
WHERE COALESCE(tool_policy->'allow_tools', '[]'::jsonb) ? 'list_task_comments'
  AND NOT COALESCE(tool_policy->'allow_tools', '[]'::jsonb) ? 'list_task_documents';
