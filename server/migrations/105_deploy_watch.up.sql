-- The deploy watch and the rollback that follows a merge.
--
-- Migration 104 gave a task the commit its pull request produced
-- (board_tasks.merge_commit_sha) and said, in its own comment, that the
-- follow-up deploy watch should key off it. This is that follow-up.

-- 1. Where an environment's application logs can be read after a deploy.
--
-- health_url answers one bit — is it up — and that bit is the only thing the
-- monitor can act on. A deploy that came up and is answering 500s on one route,
-- or is logging a failed migration on boot, is invisible to it. logs_url is the
-- other half: an endpoint the app itself exposes, read AFTER a deploy by the
-- agent watching it.
--
-- Same shape and same treatment as health_url in every respect that matters:
-- agent-writable through update_deploy_target (which still cannot touch the
-- provider, the template or the vars), destination-guarded by urlguard at write
-- time AND re-validated on every fetch — a name that resolved to a public
-- address when it was saved is free to answer 127.0.0.1 by the time it is
-- dialled.
--
-- NOT NULL DEFAULT '' exactly like health_url beside it (migration 058), so the
-- store's scan into a plain string keeps working and "no log endpoint" is the
-- empty string everywhere rather than a NULL in one column and "" in its
-- neighbour. A constant default is catalog-only on Postgres 11+, so this is not
-- a table rewrite at pod startup.
--
-- Deliberately NOT a cluster or a cloud log API. This repository has no cloud
-- knowledge and must never gain any (CLAUDE.md): no kubectl, no gcloud, no
-- provider log SDK. An HTTP endpoint the application itself serves is a fact
-- about the application, not about where it is hosted.
ALTER TABLE repository_deploy_targets ADD COLUMN IF NOT EXISTS logs_url TEXT NOT NULL DEFAULT '';

-- 2. Finding the task that released a commit.
--
-- The deploy watch and the health-window attribution both ask the same
-- question from the other end: "production is unhappy — which task put this
-- commit there?". That is a lookup by merge_commit_sha, and without an index it
-- is a sequential scan of every task in the install, run from the incident
-- ingest path on every failed probe.
--
-- Partial, because merge_commit_sha is NULL for every task that has not been
-- merged — which is most of the board, forever.
CREATE INDEX IF NOT EXISTS idx_board_tasks_merge_commit
    ON board_tasks (merge_commit_sha)
    WHERE merge_commit_sha IS NOT NULL;

-- 3. The watch/rollback tools, for the role that watches and rolls back.
--
-- Tool policies are written on agent CREATE only (an admin's customization must
-- survive a restart), so existing installs need new tools backfilled here —
-- same guarded-UPDATE pattern as migrations 081, 085, 088 and 104: re-running
-- never appends a duplicate.
--
-- QA and nobody else, for the same reason migration 104 gave the merge to QA
-- alone: QA is the role that last exercised the built product, it is the role
-- `done` wakes, and the deploy it now watches is the deploy of the merge it
-- just made. A developer that could roll production back would be able to undo
-- a release it was never asked about.
--
--   get_task_deploy_status  read-only: what happened to this task's merge
--                           commit in production.
--   get_deploy_logs         read-only: the failing Actions job's log, or the
--                           environment's own logs_url.
--   rollback_task_release   the write. Authorized in the application layer
--                           (deployops.Service.RollbackForTask), not here.
UPDATE agents
SET tool_policy = jsonb_set(
        tool_policy,
        '{allow_tools}',
        COALESCE(tool_policy->'allow_tools', '[]'::jsonb) || '["get_task_deploy_status"]'::jsonb
    )
WHERE name = 'qa-agent'
  AND NOT COALESCE(tool_policy->'allow_tools', '[]'::jsonb) ? 'get_task_deploy_status';

UPDATE agents
SET tool_policy = jsonb_set(
        tool_policy,
        '{allow_tools}',
        COALESCE(tool_policy->'allow_tools', '[]'::jsonb) || '["get_deploy_logs"]'::jsonb
    )
WHERE name = 'qa-agent'
  AND NOT COALESCE(tool_policy->'allow_tools', '[]'::jsonb) ? 'get_deploy_logs';

UPDATE agents
SET tool_policy = jsonb_set(
        tool_policy,
        '{allow_tools}',
        COALESCE(tool_policy->'allow_tools', '[]'::jsonb) || '["rollback_task_release"]'::jsonb
    )
WHERE name = 'qa-agent'
  AND NOT COALESCE(tool_policy->'allow_tools', '[]'::jsonb) ? 'rollback_task_release';

-- 4. QA subscribes to `released`, where the watch continues.
--
-- The merge happens in `done`; a successful prod deploy moves the card to
-- `released`, and the health window that follows it is watched from there. The
-- column stays terminal for everything else: the dispatcher wakes this
-- subscription only for the deploy-watch resume and the rollback dispatch
-- (board.Dispatcher.deployWatchWake), never on an ordinary comment, update or
-- move. Same backfill shape as migrations 070 and 104.
INSERT INTO agent_column_subscriptions (agent_id, column_slug, task_type_filter)
SELECT s.agent_id, 'released', s.task_type_filter
FROM agent_column_subscriptions s
JOIN agents a ON a.id = s.agent_id
WHERE s.column_slug = 'done'
  AND a.name = 'qa-agent'
ON CONFLICT (agent_id, column_slug) DO NOTHING;
