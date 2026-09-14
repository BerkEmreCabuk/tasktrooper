-- Corrective indexes for three seq-scan classes found in the migration audit:
-- a lookup index silently dropped with the teams column it shared, the two
-- reaper predicates that decide whether a tenant may scale to zero, and six
-- foreign keys Postgres does not index on its own.
--
-- Every statement is IF NOT EXISTS so re-running this file is a no-op: the
-- runner records versions in the same transaction as the work, but a tenant
-- provisioned from a future baseline dump may already have some of these.
--
-- WHY NOT `CREATE INDEX CONCURRENTLY`
-- The runner wraps every migration in a transaction, which forbids CONCURRENTLY
-- outright, and the plain build below is the right call anyway:
--   * Cost. A btree build on Postgres 16 measures ~0.3s per million rows for
--     these column widths (measured, 1M-row table: 285ms partial, 357ms single
--     uuid column, 301ms two-column). The largest table here, task_agent_runs,
--     is append-only but is one team's agent runs; a couple of seconds of held
--     ShareLock on the draining pod is the realistic worst case.
--   * Risk the other way. CONCURRENTLY waits for every transaction that can see
--     the table, which the lock_timeout in the runner cannot bound, and a build
--     interrupted by a pod kill (routine here — the reaper scales tenants to
--     zero mid-flight) leaves an INVALID index. `CREATE INDEX CONCURRENTLY
--     IF NOT EXISTS` then skips that invalid index forever: the planner never
--     uses it, nothing rebuilds it, and the migration reports success. A silent
--     permanent failure is strictly worse than two seconds of blocked writes.
--   * The lock_timeout the runner now sets means this migration cannot queue up
--     behind a long transaction and stall the table for everyone; it fails with
--     55P03 and is retried with backoff instead.

-- 038_remove_teams dropped sessions.team_id, which took idx_sessions_team_agent
-- with it — the only index that covered agent_id. Since then SessionStore
-- .ListByAgent ("WHERE agent_id = $1 ORDER BY updated_at DESC LIMIT ...") and
-- the ON DELETE CASCADE from sessions.agent_id -> agents both seq-scan
-- sessions. The column order matches the query exactly, so the sort is free.
CREATE INDEX IF NOT EXISTS idx_sessions_agent_updated
    ON sessions (agent_id, updated_at DESC);

-- The control-plane reaper runs internal/control/provision.activeRunsQuery once
-- per idle tenant per minute. Both EXISTS clauses below seq-scan tables that are
-- append-only and never pruned, so they get slower every day. On error or
-- timeout the reaper fails safe to "busy", so a tenant whose scan got slow
-- enough never scales to zero and stays billable — the cost of the missing
-- index is a bill, not just a latency number.
--
-- Partial on the same predicate the query uses: the live rows are a tiny
-- fraction of the table, so these stay small forever no matter how much
-- history accumulates. Second column matches the interval filter each query
-- applies (updated_at for runs, created_at for pipelines).
CREATE INDEX IF NOT EXISTS idx_task_agent_runs_active
    ON task_agent_runs (status, updated_at)
    WHERE status IN ('pending', 'running');

CREATE INDEX IF NOT EXISTS idx_task_pipelines_active
    ON task_pipelines (status, created_at)
    WHERE status IN ('pending', 'running');

-- Postgres indexes the referenced side of a foreign key, never the referencing
-- side. Every one of these is hit by a user-triggered DELETE — deleting a
-- repository, a task, an agent, a session run or an initiative project — and
-- each such DELETE currently seq-scans the child table once per parent row to
-- run its CASCADE or SET NULL.
CREATE INDEX IF NOT EXISTS idx_task_column_spans_repository
    ON task_column_spans (repository_id);          -- -> repositories, CASCADE

CREATE INDEX IF NOT EXISTS idx_task_pipelines_repository
    ON task_pipelines (repository_id);             -- -> repositories, CASCADE

CREATE INDEX IF NOT EXISTS idx_agent_score_events_task
    ON agent_score_events (task_id);               -- -> board_tasks, SET NULL

CREATE INDEX IF NOT EXISTS idx_task_criterion_checks_agent
    ON task_criterion_checks (agent_id);           -- -> agents, SET NULL

CREATE INDEX IF NOT EXISTS idx_task_agent_runs_session_run
    ON task_agent_runs (session_run_id);           -- -> session_runs, SET NULL

CREATE INDEX IF NOT EXISTS idx_board_tasks_initiative_project
    ON board_tasks (initiative_project_id);        -- -> projects, SET NULL
