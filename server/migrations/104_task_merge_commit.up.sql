-- The commit a task's pull request produced when it was merged, and the tool
-- that produces it.
--
-- Until now nothing merged: task PRs were opened (as drafts, which GitHub
-- refuses to merge at all) and then waited for a human to press the button, so
-- a task could reach `released` with its change still sitting on a branch. The
-- QA agent now merges the PR itself when the card reaches `done` — squash, then
-- delete the branch — and this column is where the resulting commit is
-- recorded.
--
-- Why store it rather than re-read it from GitHub:
--
--   1. It is the dispatcher's idempotency key. `done` wakes the QA agent only
--      for a task whose PR is not merged yet; without a local record of the
--      merge, every later board event on a done task would wake it again and
--      the column would loop. A GitHub round-trip inside the dispatch path is
--      not an option — dispatch runs on the request that moved the card.
--   2. It is what the deploy that follows a merge has to watch. The release
--      path dispatches its prod workflow on the repository's DEFAULT BRANCH
--      (repository.TriggerRelease -> deployRef), which is the known drift bug
--      in todo.md ("TriggerRelease main drift'ini yakalamıyor"): the default
--      branch carries everyone else's merges too, while the release gate only
--      ever proves THIS task's identity. The follow-up change that adds the
--      deploy watch/rollback should key off merge_commit_sha — the exact commit
--      this task put on the branch — instead of a branch name that has already
--      moved on. This change deliberately does not touch the release path.
--
-- Nullable with no default: "never merged" and "merged before this column
-- existed" are the same unknown, and a nullable column with no default is
-- catalog-only on Postgres 11+ — no table rewrite at pod startup.
ALTER TABLE board_tasks ADD COLUMN IF NOT EXISTS merge_commit_sha TEXT;

-- The merge tool for the role that merges. Tool policies are written on agent
-- CREATE only (an admin's customization must survive a restart), so existing
-- installs need the new tool backfilled here — same guarded-UPDATE pattern as
-- migrations 081, 085 and 088: re-running never appends a duplicate.
--
-- QA and nobody else. The developer must not merge its own branch, the
-- architect reviews it, and the PM approves the product rather than the git
-- history. QA is the last role that actually exercised the built product, and
-- `done` is the column it now owns for exactly one action.
UPDATE agents
SET tool_policy = jsonb_set(
        tool_policy,
        '{allow_tools}',
        COALESCE(tool_policy->'allow_tools', '[]'::jsonb) || '["merge_task_pull_request"]'::jsonb
    )
WHERE name = 'qa-agent'
  AND NOT COALESCE(tool_policy->'allow_tools', '[]'::jsonb) ? 'merge_task_pull_request';

-- QA subscribes to `done`, where it merges.
--
-- The role seed only sets subscriptions for an agent that has NONE, so every
-- install created before this change has qa-agent on ready_for_qa + in_qa and
-- would never learn about the third column. Same backfill shape as migration
-- 070, which added in_qa for exactly this reason.
--
-- `done` still dispatches nobody for anything else: the dispatcher wakes this
-- subscription only for a task whose pull request is recorded and not merged
-- yet, and only on a move INTO done (see board.Dispatcher.doneMergeWake). A
-- comment, an update or a second move on an already-merged task starts nothing,
-- which is what keeps the terminal column terminal.
INSERT INTO agent_column_subscriptions (agent_id, column_slug, task_type_filter)
SELECT s.agent_id, 'done', s.task_type_filter
FROM agent_column_subscriptions s
JOIN agents a ON a.id = s.agent_id
WHERE s.column_slug = 'in_qa'
  AND a.name = 'qa-agent'
ON CONFLICT (agent_id, column_slug) DO NOTHING;
