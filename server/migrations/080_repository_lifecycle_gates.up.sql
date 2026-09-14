-- The board's two terminal columns were claims nothing checked.
--
-- `done` is supposed to mean "this passed its review chain" — code review, QA
-- and UAT for a task/bug, analiz review for an analiz. Nothing enforced it:
-- in_progress -> done was an ordinary move for a human or an agent, and the
-- control plane then recorded a reviewed, tested, accepted task that no
-- reviewer, no QA round and no PM had ever seen.
--
-- `released` is supposed to mean "this is live in production". Nothing tied it
-- to a deploy either, so a task could be marked shipped while its code sat on
-- an unmerged branch.
--
-- WHY OPT-IN RATHER THAN ALWAYS ON
-- Both requirements are only honest on a board wired to satisfy them, and
-- board wiring is per-tenant and user-editable:
--   * board_columns is a table. A board that renamed or dropped in_qa/pm_uat
--     would have every task parked in front of a stage nothing can reach. (The
--     gate additionally skips any stage whose column is absent from the board,
--     but that is a safety net, not a licence to force this on.)
--   * Column subscriptions are per-install. A repository with no QA agent
--     subscribed to in_qa can never satisfy a QA requirement.
--   * A repository with no prod deploy workflow mapped records its production
--     deploy as SKIPPED — nothing executed — and a skipped pipeline is not
--     evidence of a deploy. Such a repo must leave require_release_deploy off
--     or its tasks would never leave done.
-- Defaulting both to false means this migration changes the behaviour of
-- exactly zero existing boards; an owner turns each on once their board can
-- actually satisfy it.
--
-- Two flags rather than one: enforcing the review chain needs no CI at all,
-- while enforcing the deploy needs a mapped workflow. A repo that has the
-- former and not the latter is a normal state, not a misconfiguration.
--
-- Safe on a populated table: both are BOOLEAN NOT NULL DEFAULT false, which
-- Postgres 11+ (this runs on 16) applies as a catalog-only change — no table
-- rewrite, no long ACCESS EXCLUSIVE hold, so it is safe to run at pod startup
-- against a live multi-tenant database. IF NOT EXISTS keeps a re-run a no-op
-- for tenants provisioned from a later baseline dump.
ALTER TABLE repositories
    ADD COLUMN IF NOT EXISTS require_review_chain BOOLEAN NOT NULL DEFAULT false;

ALTER TABLE repositories
    ADD COLUMN IF NOT EXISTS require_release_deploy BOOLEAN NOT NULL DEFAULT false;
