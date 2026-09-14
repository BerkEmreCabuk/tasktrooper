-- The code-review gate gets a bounded life, and a per-repository off switch.
--
-- WHAT WENT WRONG. A task.moved into code_review does not dispatch the
-- reviewing architect directly: board.Dispatcher defers it until the build/test
-- pipeline reports success, and PipelineRunner.finalize is the only thing that
-- ever re-enters the dispatcher (DispatchQA). So the gate had exactly one way to
-- open — a pipeline reaching a terminal state inside the process that started
-- it. Every way that can fail to happen wedges the card in code_review with a
-- spinner and no agent, forever:
--
--   * the pod restarts mid-pipeline. Start() calls FailStaleRunning, which
--     writes status='failed' straight to the row and fires NO side effects — no
--     need_revision move, no dispatch, nothing.
--   * the repository's GitHub Actions quota is exhausted (402/403) or no run is
--     ever created for the head SHA, so the in-process poll waits out its 30
--     minutes and finds nothing to conclude from.
--   * the webhook was push-only, so a real CI result had no way in at all.
--
-- Three columns close it.
--
-- require_pipeline_for_review — the escape hatch. Unlike its siblings
-- (require_review_chain, require_release_deploy) this defaults to TRUE, and the
-- asymmetry is deliberate: those two are new REQUIREMENTS a repo opts into,
-- this one is behaviour that has always been on and is now opt-OUT-able. A repo
-- with no CI budget left, or one that simply does not want review gated on a
-- build, turns it off and code_review dispatches immediately.
--
-- head_sha — what the reconciling poll needs to ask GitHub about a pipeline
-- whose workspace is gone and whose in-process poller died with the pod. It is
-- recorded once, when the pipeline resolves its git info, and it is the only
-- coordinate needed afterwards (owner/repo come from repositories.remote_url).
-- Rows written before this migration have '' and are resolvable only by the
-- timeout path, which is correct: nothing knows what commit they were about.
--
-- gate_reason — why the gate opened without a green build. Empty for every
-- ordinary pipeline; one of the domain.PipelineGateReason* codes when the
-- reviewer was dispatched on a timeout, on a refusal from GitHub, or because no
-- CI was configured. The UI renders it on the card so "the spinner stopped and
-- an architect appeared" is never something the user has to guess the cause of.
--
-- Additive: three columns with defaults and two partial indexes. No existing
-- row changes meaning.

ALTER TABLE repositories
    ADD COLUMN IF NOT EXISTS require_pipeline_for_review BOOLEAN NOT NULL DEFAULT true;

ALTER TABLE task_pipelines
    ADD COLUMN IF NOT EXISTS head_sha    TEXT NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS gate_reason TEXT NOT NULL DEFAULT '';

-- The sweeper's list query: every pipeline still pending/running, oldest first.
-- Partial on the two live statuses because that set is tiny and permanently so
-- (a finished pipeline leaves it and never returns), while the table itself
-- grows with every task that ever entered code_review.
CREATE INDEX IF NOT EXISTS idx_task_pipelines_unfinished
    ON task_pipelines (created_at)
    WHERE status IN ('pending', 'running');

-- The webhook's lookup: "which unfinished pipeline is this workflow_run about".
-- A workflow_run delivery carries head_sha and nothing else that identifies a
-- task, so this is the whole join. Partial for the same reason, plus rows
-- written before this migration carry '' and must not be matched by anything.
CREATE INDEX IF NOT EXISTS idx_task_pipelines_head_sha
    ON task_pipelines (repository_id, head_sha)
    WHERE head_sha <> '' AND status IN ('pending', 'running');
