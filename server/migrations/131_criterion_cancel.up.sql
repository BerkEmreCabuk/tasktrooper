-- A third answer for an acceptance criterion: cancelled, with the reason.
--
-- Until now a criterion had two states — ticked, or open — and the end-of-run
-- sweep asked the agent to tick what it had implemented and to leave the rest
-- open with a comment. That made "I decided this is out of scope" and "I forgot
-- it" the same row on the card, and both of them park the task forever: the
-- criteria gate refuses the hand-off to code_review while anything is open, so
-- a deliberately dropped criterion holds finished work in the column with no
-- way out that is not a lie (ticking something nobody implemented).
--
-- `canceled` is that way out, and `cancel_reason` is its price: a criterion may
-- only be dropped by saying why, and the reason is also posted as a task comment
-- so it appears where a human reads the story of the card. The gates treat a
-- cancelled criterion as settled, never as met — it is not ticked, it did not
-- pass review, and the card shows it struck through with its reason.
-- set_config for the reason migrations 121 and 124 give: this table carries
-- FORCE ROW LEVEL SECURITY and a migration has no tenant in it, so anything
-- that makes Postgres evaluate the policy would throw 42704. SET LOCAL
-- semantics — it dies with this transaction.
SELECT set_config('app.tenant_id', '00000000-0000-0000-0000-000000000000', true);

ALTER TABLE task_acceptance_criteria
    ADD COLUMN IF NOT EXISTS canceled BOOLEAN NOT NULL DEFAULT false,
    ADD COLUMN IF NOT EXISTS cancel_reason TEXT NOT NULL DEFAULT '';

-- No tool_policy backfill for cancel_criterion: since migration 114 an UPDATE
-- over `agents` from the migration runner matches nothing (FORCE RLS, no tenant
-- in scope). Existing installs get it from catalog.grantMissingRoleTools, which
-- runs per tenant at boot and grants it exactly where set_criterion_completed
-- is already granted — cancelling is the other half of that same decision.
