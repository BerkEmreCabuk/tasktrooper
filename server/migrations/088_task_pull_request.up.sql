-- The pull request a task's branch is reviewed in, and the chat thread a human
-- opens to talk about that task.
--
-- The PR existed nowhere in the schema. It was written once, as the free text of
-- a system comment ("Draft PR: <url>", repository/service.go createDraftPRAsync),
-- and re-derived everywhere else by asking GitHub which open PR has this branch
-- as its head (git.EnsureDraftPR -> FindOpenPR). So nothing could answer "which
-- PR is this task in?" without a working copy on disk, a GitHub token and two
-- round-trips — and a human asking an agent about "the PR you opened for this
-- task" had nothing to point it at.
--
-- pr_number is stored beside the URL rather than parsed out of it on every read:
-- every PR API call the agent needs (files, diff, review comments, replies) is
-- keyed by number, and a URL that does not parse must still be recorded (see
-- domain.ParsePullRequestNumber) instead of losing the only link we have.
--
-- Both are nullable with no default: "this task has no PR" and "nobody opened
-- one yet" are the same thing, and adding a nullable column with no default is
-- catalog-only on Postgres 11+ — no rewrite, safe against a live table at pod
-- startup.
ALTER TABLE board_tasks ADD COLUMN IF NOT EXISTS pr_url    TEXT;
ALTER TABLE board_tasks ADD COLUMN IF NOT EXISTS pr_number INTEGER;

-- sessions.task_id is what makes a chat be *about* a task rather than about the
-- repository: the workspace such a session runs in is the task's own branch
-- checkout (data/workspaces/task-<id>) instead of the shared mirror clone that
-- SyncDefaultBranch force-resets, and its prompt carries the task and its PR.
--
-- Nullable because nearly every session has no task. ON DELETE SET NULL because
-- deleting a task must not delete the conversation about it: the transcript is
-- the record of what was decided, and it outlives the card.
--
-- board_tasks.clarification_session_id already points the other way (task ->
-- chat) for the ONE thread a task asks its questions in. That column stays as
-- it is: it is written by an agent when it blocks on a question, this one when a
-- human opens the thread, and the task-chat opener adopts an existing
-- clarification thread rather than opening a second chat about the same task.
ALTER TABLE sessions ADD COLUMN IF NOT EXISTS task_id UUID REFERENCES board_tasks(id) ON DELETE SET NULL;

-- Looked up by task on every "open the chat about this task" call, which is the
-- only way that endpoint can tell reuse from create.
CREATE INDEX IF NOT EXISTS idx_sessions_task ON sessions (task_id);

-- The three PR tools for the roles that should hold them. Tool policies are
-- written on agent CREATE only (an admin's customization must survive a
-- restart), so existing installs need the new tools backfilled here — same
-- guarded-UPDATE pattern as migrations 081 and 085: re-running never appends a
-- duplicate.

-- Reading the PR (metadata, changed files, review comments, bounded diff) only
-- reads, so every role that reviews or reports on a task gets it.
UPDATE agents
SET tool_policy = jsonb_set(
        tool_policy,
        '{allow_tools}',
        COALESCE(tool_policy->'allow_tools', '[]'::jsonb) || '["get_task_pull_request"]'::jsonb
    )
WHERE name IN ('backend-developer', 'frontend-developer', 'mobile-developer',
               'system-architect', 'qa-agent', 'product-manager')
  AND NOT COALESCE(tool_policy->'allow_tools', '[]'::jsonb) ? 'get_task_pull_request';

-- Answering a reviewer on the PR is part of both sides of a review.
UPDATE agents
SET tool_policy = jsonb_set(
        tool_policy,
        '{allow_tools}',
        COALESCE(tool_policy->'allow_tools', '[]'::jsonb) || '["comment_on_pull_request"]'::jsonb
    )
WHERE name IN ('backend-developer', 'frontend-developer', 'mobile-developer', 'system-architect')
  AND NOT COALESCE(tool_policy->'allow_tools', '[]'::jsonb) ? 'comment_on_pull_request';

-- Committing and pushing the task branch goes to the implementer roles only.
-- A reviewer that could commit would put its own name on the branch it is
-- judging — the same reason runner.go skips the post-run commit in review
-- columns.
UPDATE agents
SET tool_policy = jsonb_set(
        tool_policy,
        '{allow_tools}',
        COALESCE(tool_policy->'allow_tools', '[]'::jsonb) || '["commit_task_changes"]'::jsonb
    )
WHERE name IN ('backend-developer', 'frontend-developer', 'mobile-developer')
  AND NOT COALESCE(tool_policy->'allow_tools', '[]'::jsonb) ? 'commit_task_changes';
