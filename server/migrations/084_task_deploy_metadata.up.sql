-- Deploy knowledge that only ever lived in free-text comments.
--
-- Three things a release needs and the board could not hold:
--
--   * WHAT TO DO AROUND THE DEPLOY. The release-deploy playbook told the agent
--     to post a pre-deploy checklist as a comment before calling
--     trigger_release. A comment is not a field: nothing can read it back, the
--     next release of the same task re-invents it, and a task that is released
--     by the pipeline (not by the agent) never gets one at all. before_deploy /
--     after_deploy / rollback_plan make those steps part of the task, so the
--     release path can surface them at the moment they apply.
--
--   * DEPLOY ORDER. task_relations already models "blocks", but that is a
--     planning statement about who may start work, and nothing gates on it.
--     Shipping order is a different question with a different answer: the API
--     task must be live before the client task that calls it, even though both
--     were developed in parallel and neither blocked the other. That is a new
--     relation type on the existing table rather than a new table — the shape
--     (source, target, type) is identical and the read paths already exist.
--
--   * RELEASE GROUPING. A repository with auto_release_on_done off has, today,
--     no release path whatsoever: trigger_release refuses it and nothing else
--     dispatches a prod deploy. deploy_packages is that missing path — an
--     explicitly assembled, ordered set of tasks released as one train.
--
-- All three are additive: nullable columns, a widened CHECK, and two new
-- tables. No existing row changes meaning and no existing query needs updating.

-- Nullable rather than NOT NULL DEFAULT '': "this task has no rollback plan"
-- and "nobody has written one yet" are the same thing here, and an empty
-- string would force every read path to trim before deciding whether to show
-- the section. Adding a nullable column with no default is catalog-only on
-- Postgres 11+ — no rewrite, safe against a live table at pod startup.
ALTER TABLE board_tasks ADD COLUMN IF NOT EXISTS before_deploy  TEXT;
ALTER TABLE board_tasks ADD COLUMN IF NOT EXISTS after_deploy   TEXT;
ALTER TABLE board_tasks ADD COLUMN IF NOT EXISTS rollback_plan  TEXT;

-- Migration 022 created task_relations with an inline CHECK that allowed
-- exactly one relation type, which Postgres named task_relations_relation_type_check
-- (single-column check → table_column_check). Widen it rather than drop it: the
-- constraint is what stops a typo'd relation_type from becoming a relation
-- nothing will ever match.
ALTER TABLE task_relations DROP CONSTRAINT IF EXISTS task_relations_relation_type_check;
ALTER TABLE task_relations ADD CONSTRAINT task_relations_relation_type_check
    CHECK (relation_type IN ('blocks', 'deploy_depends_on'));

-- A named, ordered release train for one repository.
--
-- status is the train's own lifecycle, distinct from any member task's column:
--   draft     — being assembled, nothing dispatched
--   releasing — at least one member's deploy is in flight
--   released  — every member has production evidence
--   failed    — a member's release was refused or its deploy failed; note says
--               which one and why, because a failed train with no reason is
--               indistinguishable from a stuck one
--   cancelled — abandoned by a human before it shipped
CREATE TABLE IF NOT EXISTS deploy_packages (
    id            UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    repository_id UUID NOT NULL REFERENCES repositories(id) ON DELETE CASCADE,
    name          TEXT NOT NULL,
    description   TEXT,
    status        TEXT NOT NULL DEFAULT 'draft'
                  CHECK (status IN ('draft', 'releasing', 'released', 'failed', 'cancelled')),
    note          TEXT,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at    TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_deploy_packages_repository ON deploy_packages(repository_id, created_at DESC);

-- Membership. position is the human's declared preference; the release path
-- still topologically sorts members by their deploy_depends_on relations, so
-- position decides only the order of members that do not depend on each other.
CREATE TABLE IF NOT EXISTS deploy_package_tasks (
    package_id UUID NOT NULL REFERENCES deploy_packages(id) ON DELETE CASCADE,
    task_id    UUID NOT NULL REFERENCES board_tasks(id) ON DELETE CASCADE,
    position   INT NOT NULL DEFAULT 0,
    PRIMARY KEY (package_id, task_id)
);

CREATE INDEX IF NOT EXISTS idx_deploy_package_tasks_task ON deploy_package_tasks(task_id);
