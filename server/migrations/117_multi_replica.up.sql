-- Make N replicas of one agent-server safe against each other.
--
-- Everything this migration adds exists because a fact that used to live in one
-- process's RAM has to live somewhere every process can see. The deployment ran
-- `replicas: 1` with a `Recreate` strategy, so a deploy was downtime for every
-- customer and a crash took everyone — and the reason it ran that way was not
-- the database, it was a handful of Go maps.
--
-- The pattern is the one already used four times over (DeviceSweeper,
-- QuotaSweeper, WorkOrderSweeper, DeploySweeper): a claim is a single statement
-- that selects a winner with FOR UPDATE SKIP LOCKED and writes the state change
-- in the same breath, so two pods asking at once produce one winner and one
-- "somebody else has it". No lease column, no owner column, no expiry to reap:
-- the transition IS the claim, and the row's own updated_at heartbeat is what
-- says the winner is still alive.

-- ---------------------------------------------------------------------------
-- task_agent_runs — the claim index
-- ---------------------------------------------------------------------------
--
-- No new columns. A run is claimed by moving it 'pending' -> 'running', which
-- the schema already expresses, and liveness is updated_at, which the runner
-- already heartbeats. What was missing is the ability to ask the two questions
-- a claim has to answer cheaply:
--
--   "is another run of this task already live?"   -> one row per task
--   "is this tenant at its plan's concurrency?"   -> count of live rows
--
-- Both are answered from the live set, so the index is on exactly that set.
-- Partial on status='running': a board's history is overwhelmingly finished
-- runs, and indexing them would be one dead entry per row forever.
CREATE INDEX IF NOT EXISTS idx_task_agent_runs_running
    ON task_agent_runs (tenant_id, task_id, updated_at)
    WHERE status = 'running';

-- ---------------------------------------------------------------------------
-- github_webhook_deliveries — dedupe that survives the process
-- ---------------------------------------------------------------------------
--
-- GitHub retries a delivery it did not get a 2xx for, and it retries to
-- whichever pod the load balancer picks. The dedupe was an in-memory map with a
-- 15-minute TTL, so a retry landing on a second replica was simply processed
-- again: a second reindex pass over the same push, a second resolve of the same
-- workflow run.
--
-- The delivery id is GitHub's own uuid (X-GitHub-Delivery) and is stable across
-- retries, which is what makes an INSERT the whole mechanism: the first pod to
-- insert wins, every other pod gets a conflict and stops. There is no read
-- before the write and therefore no window between them.
--
-- tenant_id is here for the same reason it is everywhere else — a delivery
-- belongs to the tenant whose repository it is about — but it also means two
-- tenants that somehow saw the same delivery id would not shadow each other.
CREATE TABLE IF NOT EXISTS github_webhook_deliveries (
    tenant_id   UUID NOT NULL DEFAULT current_setting('app.tenant_id')::uuid,
    delivery_id TEXT NOT NULL,
    seen_at     TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (tenant_id, delivery_id)
);

ALTER TABLE github_webhook_deliveries ENABLE ROW LEVEL SECURITY;
ALTER TABLE github_webhook_deliveries FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON github_webhook_deliveries
    USING (tenant_id = current_setting('app.tenant_id')::uuid)
    WITH CHECK (tenant_id = current_setting('app.tenant_id')::uuid);

-- The table is a ledger nobody reads back, so it needs pruning rather than
-- retention. The index is what makes the prune a range scan instead of a table
-- scan; the prune itself runs from the webhook path (see
-- repository.Service.seenDelivery), not from a cron this repo does not have.
CREATE INDEX IF NOT EXISTS idx_github_webhook_deliveries_seen
    ON github_webhook_deliveries (seen_at);

-- ---------------------------------------------------------------------------
-- task_pipelines — the same claim, for the same reason
-- ---------------------------------------------------------------------------
--
-- PipelineRunner.inflight was a per-process sync.Map that PipelineGateSweeper
-- consulted while listing every unfinished pipeline in the DATABASE. On a
-- second replica the map is empty, so the sweeper would resolve and finalize a
-- pipeline the first replica's worker was mid-poll on: two QA dispatches, two
-- column moves, two comments, for one commit.
--
-- finalize is now guarded by a status transition instead, and this index is
-- what the guard and the sweeper's listing share.
CREATE INDEX IF NOT EXISTS idx_task_pipelines_unfinished
    ON task_pipelines (tenant_id, created_at)
    WHERE status IN ('pending', 'running');
