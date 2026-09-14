# Architecture

Hexagonal (ports and adapters): all business logic lives in `application` and `domain`;
frameworks and external systems are adapters.

## Layers

```
cmd/agent-server/main.go    ← wiring: read config, build adapters, start server
internal/
  domain/                   ← Message, ToolCall, Config, AgentRequest/Response
  port/                     ← LLMClient, ToolExecutor, ToolRegistry (interfaces)
  application/
    agent/                  ← agent loop: LLM ↔ tool execution cycle
    config/                 ← config loader (koanf + YAML)
    registry/               ← tool registry implementation
  adapter/
    http/                   ← Fiber handlers, OpenAI request/response schema
    llm/                    ← LM Studio HTTP client (OpenAI-compatible)
    tools/shell/            ← run_terminal tool
    tools/web/              ← fetch_url tool
    mcp/                    ← MCP client adapter (go-sdk)
    mcpserver/              ← the /mcp endpoint CLI sessions call back on
```

## Request flow

1. `POST /v1/chat/completions` with a Bearer token.
2. `Handler.ChatCompletions` converts the OpenAI request to `[]domain.Message`.
3. `agent.Loop.Run` fetches all tool definitions from the registry (built-in + MCP).
4. `llm.Chat` is called with messages + tools.
5. Returned `tool_calls` are dispatched to the registry, results appended, loop iterates.
6. The loop exits on a plain message or max iterations; the final assistant message is
   returned in an OpenAI-compatible response.

**Retries.** An LLM error is retried twice (3 attempts total); persistent failure is a
hard error — no fallback parsing or patching of LLM output.

## MCP integration (client side)

`mcp.Manager.LoadAndRegister` iterates `tools.mcp_servers`: `stdio` spawns the server via
`mcp.CommandTransport`, `http` connects with `mcp.StreamableClientTransport`. Each
server's tools are discovered with `session.ListTools` and registered as
`mcp_<server_id>_<tool_name>` (no collisions with built-ins); calls dispatch back through
`session.CallTool`.

## Orchestration flow

With `orchestration.enabled`, a session message can trigger multi-agent orchestration:

1. `orchestrator.Router` applies a fast-path heuristic — short/simple messages skip
   orchestration unless `orchestrate: true`.
2. `orchestrator.Planner` decomposes the request with a strict JSON schema (agents,
   skills, dependencies), retrying twice on parse failure — no key patching.
3. Plan and tasks persist in `orchestration_plans` / `plan_tasks`; activity step
   `orchestration_plan_created` is recorded.
4. `orchestrator.Executor` topologically sorts tasks into waves and runs independent ones
   in parallel (`max_parallel_tasks`, errgroup).
5. Each subtask builds its system prompt from agent prompt + skills + subtask rules
   (`prompt.Builder`) and runs `agent.Loop`; failures retry twice with repair context.
6. Steps `subtask_started` / `subtask_completed` / `subtask_failed` /
   `orchestration_complete` are recorded; the final response is appended to the session
   (LLM synthesis when `orchestration.synthesis_enabled`).

Skills and orchestrator rules are agent-scoped (`/admin/agents/:id/skills`, `…/rules`);
skill create/update embeds via `llm.Embed`. Plan status: `GET /v1/runs/{runId}/plan`. See
[Orchestration Agents](orchestration-agents.md).

## QA pipeline (board)

On a move to `ready_for_qa`, `board.PipelineRunner` runs an async build/test pipeline
against the task workspace (migration 040: `task_pipelines`, `task_pipeline_jobs`). QA
dispatch is gated on green (`DispatchQA`); a failure moves the task to `need_revision`
with a system comment carrying the failing stage's log. A repository with no
job/workflow mapping records `skipped` (migration 067), which opens the gate
(`domain.PipelineStatusOpensGate`) but is never painted green — nothing was built.

Stages resolve to the repository's configured verify/build/test commands, or a
multi-language auto-detect per ecosystem marker (Go, Node, Rust, Python, Maven, Gradle);
a `Dockerfile`/`Containerfile` triggers a container build via the configured runtime
(`podman`/`docker`/auto).

Agents read the latest run with `get_pipeline_status`, and a failed report is
auto-injected into the next `need_revision` run's system prompt. API:
`GET/POST /v1/repositories/:id/tasks/:taskId/pipelines` (+ `/:pipelineId`).
`BoardTask.latest_pipeline_status` is bulk-enriched (no N+1) on list endpoints. Every FK
reachable from `repositories`/`board_tasks` cascades on delete, pipelines included.

Full breakdown, with the other board quality gates: [Orchestration
Agents](orchestration-agents.md) → "QA pipeline (migration 040)".

### The code-review gate has a bounded life (migration 107)

The gate defers the reviewing architect on a move into `code_review` until the pipeline
reports. Originally only `PipelineRunner.finalize`, **inside the process that started the
pipeline**, could open it — so a pod replaced mid-poll, a repository out of Actions
minutes (402), or a hook registered for `push` only left the card wedged with a spinner
and no agent, permanently and silently.

Four things bound it, fastest first:

1. **A per-repository off switch** — `repositories.require_pipeline_for_review` (default
   **true**, unlike its opt-in siblings: it makes existing behaviour opt-OUT-able). Off ⇒
   `code_review` dispatches immediately and the board event carries `pipeline_gate:
   gate_disabled`, so a skipped gate is never mistaken for a passed one. Set through
   `PUT /v1/repositories/:id/lifecycle-gates`.
2. **The webhook, actually wired** — the hook subscribes to `push`, `workflow_run` and
   `check_suite` (`githubapi.WebhookEvents`). A completed delivery goes
   `handler_github_webhook.go` → `repository.Service.HandleGitHubWorkflowEvent` →
   `PipelineRunner.ResolveByHeadSHA`, the join from "GitHub finished a run" to "which card
   was waiting" (`task_pipelines.head_sha`). `pull_request` is deliberately absent — the
   board opens and merges its own PRs and the gate reads run conclusions, not PR state.
3. **Hook repair** — `ReconcileWebhooksAsync` at boot installs a missing hook and
   `PATCH`es an existing one's **events list only**
   (`githubapi.ReconcileRepoWebhookEvents`): no `config` block, so no secret rotation and
   no window of rejected deliveries. It sends the union (an operator's extra event
   survives) and leaves a `*` wildcard hook alone. A converged hook costs one GET.
4. **A reconciling poll** — `board.PipelineGateSweeper` every `board.pipeline_gate_interval`
   (2m) asks GitHub about each unfinished pipeline and settles it. `PipelineRunner.inflight`
   keeps it off pipelines this process already polls, so the two never double-write jobs.

Three possible outcomes, and the difference is recorded:

- **A real result** → `finalize`: success/skipped hands the task to its reviewer, failure
  moves it to `need_revision` with the failing job's log. A red build never opens the gate,
  and at the deadline a check that already reported red still beats the clock.
- **An answer that can never come** → the gate OPENS with `task_pipelines.gate_reason`:
  `no_ci_configured`, `ci_unavailable` (`githubapi.IsCIUnavailable` — 402, or a 403 naming
  billing/quota/disabled; a plain rate limit does not count — or no run for the head SHA
  after `pipelineNoRunGrace`, 5m), or `timeout` (nothing within `board.pipeline_gate_timeout`,
  45m). The pipeline is `skipped`, never `success`; a comment says why; the event reads
  `pipeline_gate_opened`.
- **The card already moved on** → the row is settled quietly, no dispatch, no move.

`BoardTask.latest_pipeline_gate_reason` rides beside the status in the same bulk query, so
the card shows a warning glyph with the reason instead of a spinner.

The 45-minute window is derived, not chosen: it must exceed `pipelineMaxWait` (30m, the
in-process poll's budget) so a live pipeline always produces the real verdict first —
otherwise the sweeper would convert a build about to go red into an opened review gate —
plus 15 minutes of slack for a replaced pod and Actions queue time. Every other path
pre-empts it.

## Where work may start, and who is recorded as starting it

`board.Dispatcher.Dispatch` records the board event for every task change, but two
columns never produce an agent run (`isDispatchSuspendedColumn`): `blocked` (the
clarification resume path owns it) and `backlog` (not yet taken onto the board — a task
created there with an assignee used to be dispatched immediately by its own `task.created`
event). `board.Reconciler` skips the same columns. Work starts when the task is moved onto
the board.

Every `task.moved` payload carries an explicit `actor` (`agent`/`human`/`system`), and
system moves also carry a `system_reason` (`domain.MoveReason*`). Control-plane moves used
to carry no actor and rendered as "by User". The pipeline hand-off changes no column, so
its history row shows the reason sentence instead of an empty `▭ → ▭`.

## Column span ledger (migration 057)

`task_column_spans` records one uninterrupted stay of a task in one column: entry, exit,
duration, and the agent that worked it. It is the source of truth for time KPIs and for
deciding which agent a defect escape belongs to.

Spans are written from `board.Dispatcher.Dispatch` — the single place a board event row is
created — so no move path can bypass the ledger and the destination column never has to be
recovered from an event payload. The owning agent is claimed when the span's first
`task_agent_runs` row is created.

`board_tasks.clean_completion` / `completed_at` are stamped by `board.CompletionStamper` on
`done`/`released`, recording whether the task got there without entering `need_revision`;
time KPIs read only clean tasks. Review verdicts (`review_verdict` on the open span) drive
the "human after agent" review mode.

## Context optimization (large codebases)

Three layers reduce tokens: **repository mapping** (ASCII tree + skeleton signatures,
[repository-mapping.md](repository-mapping.md)), **workspace RAG** (AST chunks embedded in
Postgres, top-K retrieval, [codebase-indexing.md](codebase-indexing.md)) and the
**dependency graph** (call/import edges, BFS around a symbol,
[dependency-graph.md](dependency-graph.md)).

Session message flow: history from Postgres → optional upload RAG (`file_ids`) →
workspace/index inject (tree + chunks) → token budget (rolling summary, then trim) → agent
loop or orchestrator. The orchestrator adds planner/explorer context via
`orchestrator.ContextBuilder`; code tools (`codebase_search`, `grep_code`, `get_repo_tree`,
`get_symbol_skeleton`, `expand_symbol_context`) allow on-demand retrieval. See
[context-management.md](context-management.md).

## Deploy targets & production incidents (migration 058)

`repository_deploy_targets` holds one row per (repository, env): provider, template id,
substitution vars, health URL, rollback policy. Recipes are embedded markdown
(`internal/application/deploy/templates/*.md`, YAML frontmatter + workflow YAML) rendered
against a target — `{{var}}` placeholders are filled, `${{ … }}` Actions expressions are
left alone, an unfilled required var stays visible as `{{key — SET THIS}}`.

Production signals converge on `prod_incidents`: the alert webhook (Alertmanager / Sentry /
Cloud Monitoring / generic JSON, normalized in `prodops.Normalize`), the health monitor
(`prodops.Monitor` — two consecutive failures open an incident, a success closes it), and
failed stage/preprod/prod deploys reported by the pipeline runner.

Dedupe is a partial unique index on `(repository_id, env, fingerprint) WHERE status NOT IN
('resolved','ignored')`: an alert storm folds into one incident (occurrences++), while the
same alert months after a resolve opens a new one. Severity escalates on recurrence, never
downgrades.

`prodops.Suggest` is a pure rules engine over (incident, recent deploys, prior resolved
incidents with the same fingerprint, deploy target): a deploy finishing within 45 min
before onset yields a rollback proposal; a previously resolved fingerprint replays the fix
that worked; otherwise the error signature classifies it as config / dependency / capacity
/ code defect. An unmatched incident still returns a diagnostic checklist — never silence.

`incident_policy` decides what follows: `off` records only, `suggest` (default) opens a
diagnosis task that must stop at a written proposal, `auto_fix` lets the task carry the fix
through the board. High and critical incidents also push to the user's devices.

## Run lifetime vs pod lifetime (drain, heartbeat, stale sweep)

A board run is minutes of work on a checked-out branch; a tenant pod is replaced far more
often than that. Four defects turned every overlap into "reconciler: no progress before
stale timeout" on runs nobody had abandoned:

1. **`ensureDeployment` overwrote the pod template wholesale**, dropping annotations it did
   not own — including the `kubectl.kubernetes.io/restartedAt` a deploy had just stamped,
   which changed the pod-template-hash and fired a second rolling restart mid-run. Foreign
   annotations are merged now.
2. **SIGTERM cancelled before it drained**, so the run context was dead when the drain began.
   `server.Shutdown()` runs first; terminal run writes go through `persistCtx`
   (`context.WithoutCancel` + 15s) so even a cancelled run records its status.
3. **The runner had no drain.** `Runner.Drain(ctx)` stops taking new jobs and waits for
   in-flight ones, cancelling only if the pod's grace period expires first. The deployment
   asks for `terminationGracePeriodSeconds: 600` (GKE Autopilot clamps larger values — a
   requested 1500 landed as 600) and the bridge drains for `SHUTDOWN_GRACE` (9m) inside it.
   The tenant-manager sweeps every tenant through `EnsureTenant` at startup and hourly
   (`startTenantSpecReconciler`), or a spec change would only reach a warm pod on its next
   wake and the drain would be SIGKILLed halfway.
4. **`updated_at` froze at run start**, so `ListStale` could not tell "the pod died" from
   "still working" and failed every run longer than `reconcile_stale_after` (30m). The runner
   `Touch`es the row every `runHeartbeat` (10s), and the heartbeat is now the ONLY liveness
   authority — `SetLiveRunChecker` is gone, because it asked one process's map and reported
   every other replica's live run as abandoned. `reconcile_stale_after` is clamped to
   `maxRunStale` (18 missed beats), and `FailIfStale` re-asserts the cutoff inside the write,
   so an owner that heartbeats mid-sweep keeps its run whichever pod it is in.

The UI had the mirror-image bug: liveness inferred from the step stream alone showed a killed
run as "Live" forever. `useRunActivity` takes the run's own status, and an unfinished subtask
in a run that is over renders as interrupted.

## Migration gate & test strategy (migration 059)

A schema change is the one class of change build+test cannot judge — both stay green while
production breaks on rollout. The board detects it from the branch diff
(`domain.DetectMigrationChange` over `git diff --name-only` against the merge base,
covering golang-migrate, Flyway/Liquibase, Prisma, Alembic and Rails layouts, ignoring
vendor and testdata) and stamps `board_tasks.has_migration`. An agent cannot opt out by not
mentioning it.

`board_tasks.stage_verified_at` is written only by a **successful stage deploy** of that
task (`PipelineRunner.finalize`). `TriggerRelease` refuses `has_migration &&
stage_verified_at IS NULL` (`ErrMigrationNotStaged`) and comments why, so the block is
visible on the board rather than only in a failed API call.

`repositories.test_strategy` decides when staging is used: `local` (workspace tests only),
`stage` (default, deploy at ready_for_qa) or `per_step` (also at code_review). The
migration gate is independent — a `local` repo still cannot release an unstaged schema
change, it just dispatches the stage deploy deliberately.

## Mobile store deploy (migration 061)

Two providers (`app_store`, `google_play`), **no templates** — the five mobile ones were
deleted (2026-09-02). A repository is bound to one app picked from the connected console
(ASC `GET /v1/apps`; Play only through the Reporting API's `apps:search`, which falls back
to a typed identifier when the service account cannot reach it), and `storeops/pipeline`
generates one `scripts/mobile-release.sh` plus a thin workflow that calls it — same script,
both engines.

Channels are `internal` / `external` / `production` (`domain.StoreTracks`), promoted one
step forward only. iOS: TestFlight internal groups → external groups + Beta App Review →
App Store version. Play: `internal` → `alpha`/`beta` → `production`.

`repositories.release_engine` (`auto` | `github_actions` | `local`, migration 126) picks
where a release runs. `auto` tries Actions, then the paired Mac. Only a definite "Actions
cannot run" (no workflow, 402, billing, quota) falls through; a plain 5xx propagates. When
neither can run, `domain.ErrNoReleaseEngine` parks the card on `human_decision` — there is
no third path.

The script's build targets come from the working copy, not from a convention
(`repositories.detected_xcode_scheme` / `detected_gradle_module`, migration 127; read by
`repository.DetectBuildTargets` at import, sub-projects carry their own inside
`sub_projects`). iOS: the shared `.xcscheme` file names first (test/extension names
dropped), else the `.xcodeproj`/`.xcworkspace` name; Android: the `settings.gradle` module
applying `com.android.application`. Two candidates or none ⇒ `""`, and `""` fails
`StartBuild` with `storeops.ErrBuildTargetUnknown` (409) naming the fix, instead of
archiving a scheme that does not exist.

`mobile_store_apps` (one row per repository+platform) is the source of truth for
first-publish-vs-update: `unregistered → onboarding → test_ready → live`, with one backward
edge `test_ready → onboarding`. `mobileStoreGate` reads it on every stage/prod dispatch:
stage needs `test_ready` or `live` (`ErrMobileAppNotTestReady`), prod needs `live`
(`ErrMobileAppNotLive`); a lookup failure propagates rather than failing open.

A successful prod deploy for a store target *is* the submit for review, so
`board.PipelineRunner.markStoreSubmitted` calls `storeops.MarkSubmitted`
(`review_state = waiting_for_review`) — that write is what puts a row in the monitor's poll
gate, and a "no workflow configured" skip never does.

`storeops.Monitor` sweeps every registered app on `storeops.poll_interval` (5m):
re-verifies an `onboarding` checklist against the store APIs (advancing to `test_ready`),
detects go-live, and polls a `live` row's pending review — a rejection or halted Play
rollout ingests into `prod_incidents`. The dedupes differ: an iOS rejection is a terminal
`review_state`, so the row leaves the poll gate for good; a halted Play rollout has no such
field and is deduped by an in-memory fingerprint claim, so one still halted across a restart
is reported once more. Signing assets due within 30 days are renewed in the same sweep; a
renewal whose GitHub secret push fails is retried next sweep (the fresh expiry would
otherwise carry it out of the window forever).

Signing is system-owned: an iOS distribution cert + provisioning profile minted through App
Store Connect; an Android upload keystore generated locally (self-signed RSA-2048,
25-year), its alias read back out of the produced PKCS#12 (go-pkcs12 writes no
friendlyName), falling back to Java's default `"1"`, never a hardcoded `"upload"`. Both are
encrypted at rest in `signing_assets` with `secrets.Cipher` and pushed to the repository's
Actions secrets on every mint or renewal.

Manual first publish is forced by the APIs: ASC cannot create an app record, Play can
neither create an app nor accept its first AAB, and iOS builds need macOS runners. First
publish stops at TestFlight / Play internal testing with a guided onboarding checklist
task; once `live`, a prod deploy is a store submit with no human step — the prod workflows
never rebuild (`--skip_binary_upload`, `track_promote_to`).

Migration 061 tables: `store_credentials`, `mobile_store_apps`, `signing_assets`.

## Repository lifecycle gates (migration 080)

`done` claims "this passed its review chain" and `released` claims "this is live in
production"; nothing checked either. Two per-repository, opt-in, default-`false` flags do
(`internal/domain/lifecycle_gate.go`, `internal/application/repository/lifecyclegate.go`).

`require_review_chain` blocks a move into `done` (and into `released` when it skips `done`,
but not the ordinary `done → released` promotion) unless the task visited every stage
`domain.ReviewChainForType` requires — `code_review`/`in_qa`/`pm_uat` for `task`/`bug`,
`analiz_review` for `analiz` — with none of those stages' latest visit rejected. Evidence
is the `task_column_spans` ledger, not the current column, so rework through
`need_revision` is not punished; a stage whose column is absent from the board is skipped,
since a customized board cannot route through a column it does not have.

`require_release_deploy` blocks a move into `released` unless `task_pipelines` records a
successful `prod_deploy`, or a successful `preprod_deploy` on a repository with no prod
workflow mapped. `skipped` is never evidence. `analiz` tasks are exempt
(`domain.TaskTypeShipsCode`).

Both default off because each is only honest on a board wired for it — a repository with no
QA agent on `in_qa`, or no prod workflow, would park every task in front of something
nothing can satisfy. Both fail closed on an unreadable ledger: a check that passes when its
evidence cannot be read is not a check. `PUT /v1/repositories/:id/lifecycle-gates` arms
them independently.

## Merging the task's pull request (migration 104)

`done` used to be the end of the board's involvement while the code was still on a branch:
task PRs were drafts, nothing un-drafted them, and GitHub refuses to merge a draft — so a
task could pass every gate and reach `released` unmerged.

**Task PRs open ready for review.** `github.CreatePullRequest` sends `draft: false` and the
port method is `EnsurePullRequest`, so no caller can read "draft" and believe it. Nothing
in the codebase gated on draft state.

**The QA agent merges it, as a tool call, in `done`.** Not a server-side hook: a hook would
fire on a state change nobody was watching, at a moment nothing had re-checked the build.
QA last exercised the built product, so `done` wakes QA (`board.Dispatcher.doneMergeWake`,
narrow enough that the column stays terminal for everything else) and the agent calls
`merge_task_pull_request`: squash-merge, then delete the branch — refused unless the task is
in `done`, its PR is open and unmerged, checks are green, the review chain is satisfied
where `require_review_chain` demands it, and the PR head is still `board_tasks.verified_sha`
(`domain.VerifiedCommitMatches`, the same comparison `releaseTargetGate` makes, asked of the
PR head). The verified SHA travels to GitHub as the merge's `sha` precondition, so a push
landing between gate and merge is a 409 rather than a silent merge of unreviewed code.

**Un-drafting needs GraphQL.** REST's "Update a pull request" documents five body
parameters and `draft` is not among them — a `PATCH` with `draft: false` reports success and
changes nothing, and the merge then fails with "Draft pull requests cannot be merged" for no
visible reason. `github.MarkPullRequestReady` uses the `markPullRequestReadyForReview`
mutation (keyed by the PR's `node_id`), which is what `gh pr ready` does. It survives as a
repair path for PRs opened before this change.

`board_tasks.merge_commit_sha` records the squash commit: the dispatcher's idempotency key
(an already-merged task stops waking QA) and the commit the deploy watch monitors.

## Watching the deploy, and rolling it back (migration 105)

"Merged" and "live and working" are different facts that look alike on a card.

**The release deploys the task's own commit.** `deployRef` used to dispatch the default
branch unconditionally while every gate above it proved something about the task's commit —
a release could be gated on one commit and deploy another. It now tags `merge_commit_sha` as
`release/<short-sha>` and dispatches THAT by name (`workflow_dispatch` takes only a branch
or tag, the same constraint `deployops.Rollback` works around). The tag is derived from the
commit, not the clock, so re-releasing reuses it and GitHub's "already exists" is a success.
A task merged before migration 104, or a token that cannot create tags, falls back to the
default branch — logged at WARN, so the drift is visible.

**`trigger_release` enforces the column it always claimed.** `releaseColumnGate` refuses
anything but `done`/`released` (a re-release is legitimate) and comments on the card.

**One task-scoped abstraction over three deploy signals.** `application/deploywatch`
answers "what happened in production to THIS task's merge commit":

| Signal | Read from | The repository this is |
| --- | --- | --- |
| `actions_run` | the DEPLOY job inside an Actions run for the commit — build/test jobs in the same run are ignored, or a red unit test would read as a failed deploy | one whose deploy is an Actions job |
| `commit_status` | `GET /repos/{o}/{r}/commits/{sha}/status` — `vercel[bot]` writes `success \| Vercel` | one that deploys on push and runs no workflow |
| `deployment_status` | the GitHub Deployment opened against the commit | ditto; richer, consulted second because an unfinished deployment would read as an eternal `pending` |

No provider credentials exist anywhere in this: every signal is read from GitHub, which all
these hosts already write to. Which job is "the deploy" is the repository's own
`prod_deploy`/`preprod_deploy` mapping when it has one, and a narrow name heuristic
otherwise. Nothing found is `no_signal` — an answer, not a failure and not a wait.

**A pending deploy parks; it never blocks.** `get_task_deploy_status` returns a
`domain.ResourceBlock{Resource: "deploy_watch"}`, the loop stops the turn, the runner parks
the card, and `board.DeploySweeper` re-dispatches when GitHub says the deploy settled. This
is the device park's mechanism with one structural difference: the device is a QUEUE (one
phone, any release frees any waiter), while two parked deploys are different things and
either may settle first — so the sweeper lists without claiming
(`ListBlockedByResource`), asks about each, and claims only the ready ones
(`TakeBlockedResourceTask`). Waiting costs one API call per parked task per pass and zero
LLM tokens.

**The terminal columns stay terminal.** `deployWatchWake` is keyed on a payload key only
the sweeper and the rollback dispatcher write (`domain.EventPayloadResumedResource`). A
state-based condition ("in done, merged, deploy unconfirmed") would be reproduced by every
later comment on the card and `done` would wake QA forever.

**The post-release health window makes attribution specific.** `prodops/remedy.go`'s
45-minute correlation asks about the ENVIRONMENT, so it can only produce "roll back the last
release", with no card and nobody to wake. `deploywatch.AttributeRelease` asks about a
COMMIT: the newest *successful* deployment run for this env, finished inside
`deploy_ops.health_window` (15m) before the incident, carries a head SHA; that SHA is some
task's `merge_commit_sha`; that task has a key, a rollback plan and an owning role. A FAILED
deploy is skipped — it left production on the previous commit. Unattributable is `ok=false`,
never an error: the generic remedy still fires.

**`deploy_targets.auto_rollback` stops being decorative.** `true` → the attributed task's
owner is woken with the incident and the task's rollback runbook; `false` → the incident is
opened, the proposal is written on the card, and a human confirms through the unchanged HTTP
endpoint. A missing or unreadable target reads as `false`.

**Two rollback mechanisms, chosen by what the repository has.** With a deploy workflow:
`deployops.Service.Rollback` (tag the last known-good commit, `workflow_dispatch` at that
tag). Without one the repository deploys on push, so what redeploys it is a new commit and
the rollback is `git revert` of the merge commit plus a push — a plain revert, since a squash
merge is single-parent (`-m` is for a true merge and fails here). It never force-pushes, and
a conflicting revert is ABORTED rather than resolved: guessing which side wins in production,
unattended, is not a thing to do. A failed push says so loudly — the revert exists locally and
production is UNCHANGED.

**The agent actor does not type a confirmation phrase.** The human endpoint's
`Confirm == repository name` guards against a misclick; an agent asked to type the name reads
it off the record it already holds, which confirms nothing and looks like confirmation in the
audit log. `deployops.Service.RollbackForTask` — a separate method, not a `SkipConfirm` flag —
swaps the ritual for four facts checked against the database: the task's merge commit must be
what the environment is running (`assertOwnsLiveRelease`, failing CLOSED on an unreadable
ledger), `auto_rollback` must be on, the claimed trigger must be real (`deploy_failed` is
re-verified against the watch), and the audit entry names the card, commit, trigger and role.
`Rollback` (the human path) is unchanged.

**The half no mechanism can perform is reported, never skipped.** `git revert` does not
reverse a migration, turn a feature flag off, purge a CDN or un-send anything. What applies is
in `rollback_plan` / `before_deploy` / `after_deploy`, previously read only on the way OUT and
now fed back in as the rollback runbook comment the run reads; every rollback result carries
`manual_steps` the agent must perform or explicitly report.

`deploy_ops.monitor_enabled` defaults to **true**: `deployment_runs` is what the rollback
reads to find the last good commit and what attribution reads to decide whose release is
live, and with the monitor off both fall back to "no evidence". Cost is bounded — one API
call per (repository × environment) with both a target and a mapped workflow, per sweep.

## Work order and the analysis reference (migration 106)

**`blocks` now blocks.** Its only reader was `repository.Service.validateMoveAllowed`, which
refuses a MOVE into `todo`/`in_progress` — catching a human dragging a card and nothing else,
while a task created straight into `todo` with an open blocker, a reconciler sweep, a sweeper
hand-back or an assignment event all reach the dispatcher without a move. The gate is now
`board.WorkOrder`, called from `Dispatcher.Dispatch` after the board event is written
(history stays complete) and before any agent is resolved, for `todo` and `in_progress` only
— the same pair the move guard uses, so a finished change in `code_review` is never stranded
behind a dependency it no longer has.

It is a **park**, not a refusal: a refused dispatch leaves the card looking unstarted with
nothing saying why. It is the fourth `domain.ResourceBlock` resource (`work_order`, beside
`mobile_device`, `claude_code_quota`, `deploy_watch`) and the odd one out in what it waits
for — the other three are facts about the world outside the board. Everything else is
identical: `blocked_resource`, the `blocked` column with the reason, a comment naming every
blocker, and a sweeper.

`board.WorkOrderSweeper` (1 minute — the shortest of the four: one indexed query against the
same database) is shaped like `DeploySweeper`, since the park is per-TASK: list without
claiming, ask, take only the free ones. It asks the relation graph itself —
`ListBlockingSources` returns the UNFINISHED sources of a task's `blocks` rows, so an empty
answer *is* "everything it waited for is done". That one query also covers both ways a
blocker can stop existing: a deleted task takes its `task_relations` rows with it (ON DELETE
CASCADE, migration 022), and a blocker returning to `in_progress` reappears, so the park is a
standing question rather than a one-off verdict. An unreadable graph fails closed.

Direction is unchanged: a `blocks` row stores the BLOCKER as `source_task_id`, the opposite
arrow to `deploy_depends_on`, and flipping it would invert every stored row. No tool or
endpoint asks a caller to think in it — they take `blocked_by`. Cycles are refused where the
edge is written, with the chain in the message.

**`derived_from` is where the spec went.** An analiz run commits nothing; its spec and plan
are `task_documents` on the analiz task and nowhere else, so implementation tasks had no
route to their own specification. The relation is that route — source = implementation task,
target = analiz task, same direction as `deploy_depends_on` — and deliberately not an
ordering relation: `blocks` would re-gate a start a human already approved in
`analiz_review`, and `deploy_depends_on` would refuse to release the implementation until a
task that ships no code reached production.

`repository.Service.AnalysisReferences` resolves it in one call and `Runner.analysisContext`
renders the documents as a system message after the trigger and **before** the diff and PR
blocks: those say what has been done to the task, this says what the task is supposed to be.
Injected on every run, not only the first — a revision run fixes against the same spec and a
reviewer judges against it. The block names `list_task_documents` as the way to re-read a
truncated plan.

### Loop guards — the fifth resource, human_decision

`ResourceHumanDecision = "human_decision"` (`internal/domain/resource_block.go`) — no
sweeper claims it; only a human moving the card out of `blocked` clears it. Same park
mechanics as the other four: `BlockOnResource`, `blocked_origin_column`, a ParkJournal row.

| Guard | File | Called from | Parks when | Reason |
| --- | --- | --- | --- | --- |
| `PipelineBounceGuard` | `internal/application/board/pipeline_bounce_guard.go` | `PipelineRunner.finalize`, before `reportPipelineFailure` | failed pipeline for a head SHA that already has a prior genuine failed pipeline (same SHA, real provider verdict, created after the last human board event) | `pipeline_loop_parked` |
| `ReviewLoopGuard` | `internal/application/board/review_loop_guard.go` | `Dispatcher.Dispatch`, after the work-order gate | a `task.moved` into `need_revision` that is the `maxReviewLoopEntries`th (3) since the last human event (explicit `to_column` only; reconciler/sweeper synthetic moves ignored; 500-event window; fails open) | `review_loop_parked` |

Motivation: CI billing-blocked → identical red every cycle → 11 review cycles observed on
one task. `PipelineBounceGuard` posts its comment once, re-reading the task first — it never
overwrites another resource's park. Web: `blockedResource.human_decision` label and history
reasons exist in both locales (web repo).

## Claude Code executor and the quota park (migrations 101, 103)

The `claude_code` provider (`domain.LLMProviderClaudeCode`) adds a second run path beside
`agentLoop.RunTask`, chosen through `port.TaskExecutor` (one `Supports`, one `Execute` taking
a `domain.TaskExecution`). The runner branches on the PROVIDER, not on "is there an
executor":

```
domain.RequiresHostExecutor(agent.ProviderType)  →  executor (or a clear failure)
r.orchSvc != nil                                 →  RunSolo        (unchanged)
default                                          →  agentLoop.RunTask (unchanged)
```

Keying on the provider is what makes a host with no CLI say *"claude code binary not
available on this host…"* instead of falling through to a loop that would open an HTTP
connection to a provider with no endpoint. The executor is registered in `platform/runtime`
only when the binary resolves on PATH (`CLAUDE_CODE_BIN`).

**Nothing else about the run changes.** The workspace clone and `tt-<key>` checkout still
happen before; grounding checks, verify gate, commit/push, PR and column advance still happen
after. The executor reports its tool calls into `registry.ToolUsage` and its tokens into
`usage.TokenUsage` — the same accumulators the loop feeds — so the run row, KPIs and
grounding gates read a CLI-worked task like a loop-worked one. The CLI's native tool names
are mapped onto TaskTrooper equivalents for that ledger (`Read` → `read_file`, `Grep` →
`grep_code`, …); without it an analiz run would be rejected for never having read the
repository it read. `total_cost_usd` is logged and traced but deliberately **not** recorded
to `llm_usage`: those tokens were paid for by a flat-rate subscription.

### Chat on the same seam (migration 103)

Chat used to go straight to the agent loop whatever the provider was, so a `claude_code`
chat turn reached the OpenAI-compatible client and reported `unsupported protocol scheme ""`.
It now branches like the board's, on the provider, ahead of the orchestrator:

```
domain.RequiresHostExecutor(agent.ProviderType)  →  chatExecutor (or a clear failure)
useOrchestrator                                  →  RunSolo / RunMulti (unchanged)
default                                          →  agentLoop.Run / RunStream (unchanged)
```

`port.ChatExecutor` is a second interface beside `TaskExecutor` (the board has no use for
streaming, the session service none for `Execute`); `*claudecode.Executor` satisfies both and
`platform/runtime` hands **the same instance** to both callers, so a chat turn and a board
task queue against one concurrency cap.

Three things make a chat different, all in `domain.ChatExecution`:

- **Continuity.** The first turn flattens the assembled history; every turn after runs
  `claude -p --resume <id> "<what the user typed>"` and sends nothing else — re-flattening a
  growing transcript would pay for the whole conversation every message *and* hand the model
  its own remembered context back as a fresh instruction. Migration 103 puts
  `sessions.cli_session_id` on the chat row (in the database, so a restart does not turn every
  open chat into a first turn), written after **every** turn including a failed one. A
  `--resume` the CLI refuses falls back to a fresh session built from the stored transcript.
- **Streaming.** Assistant text goes into the same `capture` callback `RunStream` feeds, so
  the SSE handler and every client are untouched. `streamingSink` fires `agent.SegmentBreak`
  when text is followed by a tool call — the same `reasoning_end` frame a streamed loop turn
  emits — or the browser would render the narration as the reply and then swap it for the
  shorter persisted text.
- **Tools.** A per-**turn** MCP token, minted after the concurrency slot and revoked when the
  turn ends, serving the chat's computed `workspacePolicy`. Per turn, not per conversation: a
  thread can stay open for days and a credential for this tenant's board tools must not be
  live while nobody is talking.

**Quota in a chat is not a park** — there is no card and no sweeper, only the person who
pressed enter. The `*domain.QuotaBlock` becomes a sentence for them
(`domain.NewQuotaNotice`, naming the reset in local time), riding the transcript under
`RateLimitNoticePrefix` and the SSE frame under `type: "rate_limited"`, reusing what a
provider rate limit already has.

### Every other agentic path: the router (`agent.Router`)

Two call sites consulting the seam left about twenty that did not — so a `claude_code` agent
could implement a task but not fix its own red build, settle its criteria, hand off a review
verdict, run a subtask, profile a repository or reflect on itself.

`agent.Router` implements `agent.Runner` (the loop's own three methods as an interface) and
every agentic consumer is handed it instead of the bare loop — `board.Runner`,
`orchestrator.Service`/`Executor`, `repoprofile.Service`, `evolution.Service`:

```
provider is host-executed AND an executor is wired  →  executor.Execute
provider is host-executed AND none is wired         →  ErrHostExecutedProvider
otherwise                                           →  the HTTP loop, unchanged
```

The one thing a child process needs and the loop's signature does not carry is a directory,
read off the run context (`registry.EffectiveWorkspaceDir`) — the same value the loop's tools
are scoped to, so a CLI session and a loop run work in the same tree. **No workspace is a
refusal, not a guess**: running in the server's own directory would let the session edit the
tree it is hosted from. The single exception opts in explicitly
(`agent.WithScratchWorkspace`) and is self-reflection with web research, which touches no
repository and gets a temporary directory removed afterwards. `agent.WithCLILabel` names the
session in logs and at the MCP endpoint ("tt-7 verify-fix"); the loop ignores it.

The board runner keeps its own explicit `switch` and `port.TaskExecutor` field — its
host-executed case comes first, so the router's `default` only sees HTTP providers.

`RunStream` deliberately stays on the loop even for a host-executed provider (where the
loop's guard refuses it): a streamed CLI turn needs `port.ChatExecutor`, which the session
service consults directly. `Router.SupportsHostExecution` is what `catalog.Service` asks
before an agent may be **saved** onto a host-executed provider — one error at the moment of
choice replaces ten runtime failures that each describe their own symptom.

### The guard (and the fallback that used to be here)

`domain.ErrHostExecutedProvider` fails fast at `agent.Loop.run` / `RunStream`, before the
retry loop — a malformed URL looks transient to the retry classifier, which is why the
original error was said three times.

`llm.MultiProviderClient.Chat` / `ChatStream` is the last point the request is still a Go
value, and `guardHostExecuted` refuses **every** request naming a host-executed provider:

- **Agentic** (carries `Tools`) — a run that should have been dispatched by the router.
  `domain.ErrHostExecutedProvider`. Unguarded this is worse than a failure: with no entry in
  the client map, `resolve` falls through to `m.fallback` and the run would be *sent*, to
  another provider's endpoint on another provider's key.
- **Utility** (no tools; a schema extraction, summary, commit message, judge verdict) —
  `errHostExecutedUtility`, naming **which step** could not run (the schema name —
  `planner_output`, `golden_gate_verdict` — or the call site), **that the agent runs on
  Claude Code**, the **concrete reason** where there is one, and the **two fixes**: configure
  an HTTP provider for this agent, or turn the step off.

**This used to be a reroute, and removing it was deliberate.** A utility call was sent to the
tenant's active default HTTP provider with the model blanked. The reasoning — the CLI cannot
serve it anyway — was sound and the conclusion wrong: the default provider is one the operator
did not choose *for this agent*, so every reroute converted "this agent cannot serve this
step", which is true, specific and fixable, into a 404 from a provider nobody was thinking
about.

Both refusals wrap **`domain.ErrHostExecutedUnservable`**, and callers do two things with it:
**do not retry** (`llmretry.Classify` returns `Stop`, one check where the retry policy lives
rather than at twelve call sites each inside a loop built for a flaky endpoint) and **do not
degrade quietly** (a load-bearing step fails the run with the message attached; an optional one
degrades but logs *what* it skipped and *why*, with `permanent: true` separating "this agent
can never do this" from "the endpoint had a bad minute"). Logged at WARN with the provider,
model, named call and source location. The same sentence is what `session.runHostExecutedTurn`
returns when the executor is absent — the ordinary state of every cloud pod.

### Which paths run on the CLI, and which need an HTTP provider

On a host **with** a runner, for an agent on `claude_code`:

| Path | Engine | Why |
| --- | --- | --- |
| board run (`board/runner.go`) | CLI | the original seam; explicit switch |
| build-gate fix round (`board/verify.go`) | CLI | router; runs in the task's own checkout |
| criteria sweep (`board/criteria_sweep.go`) | CLI | router |
| review verdict sweep + finalize (`board/review_sweep.go`) | CLI | router |
| orchestrator subtask (`orchestrator/executor.go`) | CLI | router; subtask workspace |
| repository profile refresh (`repoprofile/service.go`) | CLI | router; repo root, read-only tools |
| agent reflection with `allow_web_research` (`evolution/reflect.go`) | CLI | router; scratch workspace |
| chat turn (`session/`) | CLI | `port.ChatExecutor`; resumes the CLI session |

And the steps needing an HTTP provider configured **for this agent** — previously rerouted to
the tenant default, now refused, so "if refused" is what the operator sees:

| step (needs HTTP) | load-bearing? | if refused |
| --- | --- | --- |
| intake, planner (`orchestrator/{intake,planner}.go`) | **yes** | the run FAILS. `pipelineStepError` drops the misleading "after 3 attempts" for a permanent refusal |
| replanner (`orchestrator/replanner.go`) | no | repair loop abandoned; plan lands on `PlanStatusIncomplete`, skip logged at ERROR when permanent |
| verifier (`orchestrator/verifier.go`) | no (by design) | stays "inconclusive" — results already produced are never discarded — logged at ERROR **and** appended: *"⚠️ Verification did not run for this plan: …"*, because it will be skipped every run until a setting changes |
| synthesize (`orchestrator/service.go`) | no | the raw formatted task results stand; the error is now logged |
| golden suite (`evolution/golden.go`) | **yes** | abandoned at the first refusal (`goldenRun.Unservable`) rather than grinding to `Evaluated == 0` |
| golden gate judge (`evolution/golden.go`) | **yes** | the change set **REVERTS**. It used to be *kept* "on non-regression" — two rates of 0/0 satisfy `after >= before` |
| reflection without web research (`evolution/reflect.go`) | **yes** | propagated as `reflection llm call failed` |
| memory promotion / save classification (`evolution/promote.go`) | no | saved as a plain memory; the skip is now logged |
| commit message rewrite (`board/commitmsg.go`) | no | the commit lands with the task's own title and summary, logged with `permanent` |
| rolling summarize (`context/summarize.go`) | no | the loop drops the oldest messages instead of condensing, logged with `permanent` |
| loop wrap-up (`agent/loop.go`) | no | the run's own last message stands (unreachable for a CLI run) |

`appcontext.SummarizeRollingFor` takes a provider for this reason: model and provider are one
decision, and a summarize call naming the model alone was routed at the tenant default, which
answers 400 to a name that means nothing to it.

**Billing.** Flat-rate CLI tokens go to `usage.TokenUsage` and never to `llm_usage`, so they
never touch the tenant's USD budget. The HTTP steps above are metered API calls and DO bill,
through the recording client. A `claude_code` run row can legitimately mix billed and unbilled
tokens: the work was free, the JSON-shaped bookkeeping was not.

### The quota park

A CLI session stopped by the subscription's usage limit is a fact about a billing window, not
about the work. Failing the run would spend one of the task's three consecutive-failure lives
and throw away the session holding the half-finished change. So it is a **park**, modelled on
the device park, with one difference that shapes the rest:

| | device (`mobile_device`) | quota (`claude_code_quota`) |
|---|---|---|
| carried as | `AgentResponse.ResourceBlock` (a tool said "not now") | `error` — `*domain.QuotaBlock` (the run produced nothing to carry it on) |
| released by | probing the hub: is a phone free | the clock: has the recorded reset passed |
| state lives in | `board_tasks.blocked_resource` | that, **plus** `task_agent_runs.quota_resume_at` / `cli_session_id` |
| sweeps on boot | no — an idle hub may be another pod's lease | yes — a recorded reset time survives a restart |

Migration 101 adds those two columns (partial index on `quota_resume_at`). They are on the row
rather than in memory because that is what makes the park survive a restart: `QuotaSweeper`
(1-minute) asks the database, and `BoardTaskStore.TakeQuotaResumable` does the due check and
the unpark in ONE statement against the task's *latest* parked run, so a task parked twice
cannot resume on the first park's expiry. One task per claim (`FOR UPDATE SKIP LOCKED`), but
the sweep loops until nothing is due or it hits `quotaSweepBatchCap` — unlike the single
phone there is no contention between two tasks whose reset has passed.

The park is a board move like any other: a `task.moved` event (`from_column` → `blocked`,
actor `system`, reason `quota_exhausted`) and the open column span closed through
`board.ParkJournal`. It does NOT go through `Dispatcher.Dispatch` — a blocked task is
dispatch-suspended anyway, dispatching would push a notification for a card that just
stopped, and the park happens inside the very run the dispatcher started.

The resume is a *continuation*: the parked run wrote its `cli_session_id`, the resuming run
(a new row) finds it in the task's previous runs and passes it as `ResumeSessionID`, and the
executor runs `claude -p --resume <id> <short continue prompt>` in the same workspace — no
persona, no project context, no re-stated task, all of which the session holds and would read
as a competing instruction. The id is only handed to the SAME agent whose park recorded it
(`latestCLISession`), or a column dispatching to several agents would start two
`claude --resume <same id>` processes in one workspace.

Two brakes, because a park costs the task nothing and a *repeating* park is therefore
invisible:

- the limit is only recognised on a session that FAILED (`session.failed`) — it is detected
  from text, and a successful run's answer can legitimately quote the phrase;
- five consecutive parks on one task (`maxConsecutiveQuotaParks`) turn the sixth into an
  ordinary failure, so a false positive surfaces within a day instead of cycling forever.

### The tool endpoint (MCP)

A CLI session arrives with the CLI's own tools and no idea a board exists — so without
something more, a `claude_code` run cannot move its card, tick a criterion, comment on its PR
or search the semantic index, and its `domain.ToolPolicy` is a statement rather than a rule.
`internal/adapter/mcpserver` serves TaskTrooper's own tools over MCP streamable HTTP at
**`POST /mcp`** on the server's own port.

```
board run ── executor ── claude -p --mcp-config <per-run file> ─┐
                                                                │ Bearer <run token>
platform/runtime ── RunTokenRegistry ── adapter/mcpserver ◄──────┘
                                             │
                                             └─ registry.ExecuteWithPolicy(run ctx, call, run policy)
```

**Per-run tokens.** `RunTokenRegistry` (in-memory mutex map) mints 32 bytes of `crypto/rand`
per run and stores the runner's `runCtx`, the run's **tenant** and its policy. The token is
written into a 0600 file — by this process for a local session, by the runner for a remote one
— never onto a command line, which is world-readable in `ps` — and revoked on every exit path:
a finished session, a failure, the quota park, a dropped tunnel. In memory is enough precisely
because of that: a resumed park is a new run row with a fresh token, and a restart is the
strongest revocation there is. An unknown, revoked or expired token gets `401` with a JSON-RPC
error body.

`Run.ExpiresAt` is an absolute ceiling, set **only** for a token that leaves this machine
(remote), at `run_timeout + 15m` — past the session's own deadline, because a 401 mid-session
makes the CLI report `requires re-authorization` and abandon the server for the rest of the
run. A cancelled `Run.Ctx` deliberately does **not** invalidate the token, for that same
reason: the call must fail as a cancelled call, not as an auth failure.

Storing the run's *context* is what makes a CLI session's tool call indistinguishable from a
loop run's downstream: audit rows, the session action ledger, the tool-usage counters the
grounding gates read and the activity recorder all take attribution from it, and it carries
the run's cancellation, so a stopped run's in-flight tool call dies with it.

**What is served.** `DefinitionsForPolicy(run policy)` minus two groups:

| Dropped | Why |
|---|---|
| `run_terminal`, `read_file`, `write_file`, `edit_file`, `edit_lines`, `delete_file`, `move_file`, `grep_code`, `get_repo_tree` | The CLI's Bash/Read/Write/Edit/Grep/Glob are better at exactly these and are what the model was trained against. Two tools for one job is the classic way to make a model pick the worse one |
| `ask_user` | It does not return a result — it parks the run on a human answer, and a live session would sit on an open call while the run around it was parked. Clarification for `claude_code` runs needs a suspendable session first |

`codebase_search`, `get_symbol_skeleton` and `expand_symbol_context` deliberately **stay**:
they are backed by the semantic index and the CLI has no equivalent. Every board and domain
tool stays too. The same predicate gates `tools/call` — filtering an advertised list is not
access control.

**Mounted without the auth middlewares.** `/mcp` is neither `/v1` nor `/admin` and
`isPublicPath` names it explicitly: the caller is a CLI session holding *none* of this
server's credentials, and it authenticates with the per-run bearer token alone.
`tenantMiddleware` therefore never runs for it and **there is no signed header here to read**
— the tenant is bound into the token at mint time (`Run.Tenant`, from the board runner's
`runCtx`) and re-applied per call by `Run.Scoped()`. No valid token ⇒ `401`; never a default
tenant. `SetLoopbackOnly` follows where the session runs, not whether this is cloud:

| Executor | Loopback-only | Why |
|---|---|---|
| local, cloud pod | **on** | pod binds `0.0.0.0`; the only client is a `claude` child at 127.0.0.1 |
| remote (Mac) | **off** | every legitimate call arrives from the gateway; the token is the whole check |
| desktop / self-hosted | either | the listener binds 127.0.0.1 already |

`c.IP()` is the peer's real address (the fiber app is built with no `ProxyHeader`).

The session's own tool calls are recorded **once**: a `mcp__tasktrooper__*` call is counted
and traced by the registry when this endpoint executes it, so the executor's stream reader
skips those names (`claudecode/trace.go`).

**The session is pinned to this endpoint and told what it holds.** Two CLI behaviours made a
served tool as good as absent, handled where the session is spawned
(`claudecode/claudecode.go`, `claudecode/mcp.go`):

- `--strict-mcp-config` accompanies `--mcp-config`, and `--setting-sources` is `project,local`
  (config: `claude_code.setting_sources`). Otherwise the session inherits the operator's
  personal MCP servers, hooks and plugins: their hundreds of tools push the CLI past the
  threshold where it hides schemas behind `ToolSearch`, and the run spends turns *searching*
  for tools it already has.
- The system prompt carries a **tool manifest** — the exact `mcp__tasktrooper__<name>` strings
  this policy is served, from the same `DefinitionsForPolicy` call `tools/list` uses
  (`mcpserver.ServedToolNames`). Every other prompt here names tools as the registry does
  (`set_criterion_completed`), which is not a name any session can call. A resumed session is
  skipped: it already holds the definitions.
- `initGuard` fails the run when `system/init` lists MCP servers and `tasktrooper` is **not**
  among them. Absence only: `pending` is a handshake still in flight. Left running, such a
  session works on native tools, returns a plausible summary, is refused by the criteria gate
  and is dispatched again — silently, on the subscription. The init event is logged once per
  session (servers, statuses, native tool count).

**Hand-rolled, not the go-sdk.** The repo uses `modelcontextprotocol/go-sdk` as a *client*
(`adapter/mcp`), but not here: the HTTP surface is Fiber/fasthttp (the SDK's handler is an
`http.Handler`, so it would arrive through an adaptor that buffers the response, paying for
SSE and session machinery only to switch both off), the tool surface is per-run and would
need a whole `*mcp.Server` per bearer token, `Server.AddTool` *panics* on a non-object schema
and ours come from ~60 independent executors, and its schema validation sits between the
client and executors that already validate their arguments. What is left is one client, one
transport and five methods — `initialize`, `notifications/initialized`, `tools/list`,
`tools/call`, `ping` — answered as plain `application/json`. No SSE and no resumability
because there is nothing to push. `GET`/`DELETE` answer `405` rather than falling through to
the SPA, which would hand a protocol client an HTML page.

A tool failure comes back as `isError: true` with the error text, **not** as a JSON-RPC error:
the model picked the arguments, so the model has to read what was wrong with them. Results
carrying images (`mobile_screenshot`, `browser_screenshot`) map onto MCP `image` content next
to their text.

## Local simulators and emulators (migration 102)

The `mobile_*` tools were written against a physical Android phone reached through the
cluster's adb bridge (`adapter/deviceagent`, `MOBILE_BRIDGE_URL`) and driven by an Appium hub
in the cluster; iOS was refused by name, because XCUITest needs a macOS host with Xcode. The
same binary now also runs on an operator's Mac, where two more devices exist that are not
reachable that way — so a registration gained a **kind**, and the kind is the only thing that
changes:

| | `remote_adb` (default) | `ios_simulator` | `android_emulator` |
|---|---|---|---|
| attached by | the bridge sidecar, over HTTP | `simctl boot` + `bootstatus -b` | `emulator -avd … -no-window -no-audio`, then `wait-for-device` + `sys.boot_completed` |
| `device_addr` | the phone's tailnet host:port | the simulator UDID | the AVD name |
| `device_udid` | the loopback port the bridge allocated | the same simulator UDID | the adb serial it booted on (`emulator-5554`) |
| detached by | `adb disconnect` via the bridge | `simctl shutdown` | `adb -s … emu kill` |
| Appium caps | `Android` / `UiAutomator2` | `iOS` / `XCUITest`, `appium:bundleId` | identical to `remote_adb` |
| platform version | `MOBILE_PLATFORM_VERSION` or the row | derived from the simctl runtime | as `remote_adb` |

**Everything else is deliberately identical.** The eleven tools, the per-device lease, the
`domain.ResourceBlock` park and `DeviceSweeper`, the deploy-target app guard, the QA grounding
gates and the role prompts are untouched — an agent cannot tell which kind it is driving,
which is the point of doing this as a kind rather than a second tool surface. Inside the tool
layer the difference is confined to `adapter/tools/mobile.capabilitiesFor`; `Pool` needed no
change, because it keys sessions on the UDID.

**`adapter/localdevice`** is the local twin of `deviceagent`: same four questions (what is
there, attach it, is it up, detach it), answered by running a binary on this machine. It
resolves `xcrun`, `adb` and `emulator` once at boot — PATH first, then
`ANDROID_HOME`/`ANDROID_SDK_ROOT` and the standard Android Studio location — and a host that
resolves none reports no capabilities, exactly as a Linux node behaves. `xcrun` is
additionally gated on `GOOS == darwin`, so a shim cannot make a Linux box claim simulators.

**There is no shell in that package.** Every command is `exec.Command` with a validated
argument vector, validated (`ValidateSimulatorUDID`, `ValidateAVDName`,
`ValidateEmulatorSerial`) *before* the exec — the arguments arrive over the settings API, so
interpolation into `sh -c` would be remote command execution on the operator's laptop. AVD
names may not begin with `-` or `.` (`emulator -avd --help` is argument injection even with no
shell, because the emulator parses its own argv), and an emulator serial must match
`emulator-<port>`, so nothing here can `emu kill` a plugged-in phone.

**The iOS refusal is lifted per host, not removed.** `domain.ValidPlatform` still refuses iOS
for `remote_adb`; the simulator path is gated on `LocalHost.SupportsIOSSimulators`, a question
about the machine rather than the requested value.

**`GET /v1/settings/mobile-devices/local-catalog`** reports what the host could drive
(`{"ios":[{udid,name,runtime}],"android":[avd names]}`), because a local device cannot be
typed in: a simulator is a UUID and an AVD an exact SDK name, and both fail late and
unhelpfully when mistyped. Registration validates against the same source. Two empty arrays on
a host with neither, as a `200` rather than a `404`.

Migration 102 adds `mobile_devices.kind` with `DEFAULT 'remote_adb'` — the default *is* the
correct backfill, since a bridge phone is the only kind that could have been registered
before. A blank kind normalises through `domain.MobileDevice.DeviceKind()`. Tool registration
is still driven by the *effective* devices: `MOBILE_BRIDGE_URL` being unset (the normal case
on a Mac) disables nothing, because the bridge only ever served one of the three kinds.

## Mobile work reaches the assignee's Mac (`mobile.*`)

A Linux pod cannot run an iOS simulator. `adapter/localdevice` execs `xcrun`/`adb` on THIS host,
so in the cloud it is replaced — not disabled — by the same four questions asked over the tunnel.

| Question | Local | Cloud |
| --- | --- | --- |
| what is there | `localdevice.Host` | `runner.MobileHost` → `GET mobile.devices` (per member, never cached) |
| attach / detach | `simctl` / `emulator` | `POST mobile.boot` / `mobile.shutdown`, both idempotent |
| drive it | `mobile.Pool` → cluster Appium | `mobile.Fleet` → `ANY mobile.appium/…`, proxied verbatim |
| release a park | `DeviceSweeper` (one hub) | `MacDeviceSweeper` (one probe per MEMBER, one resume per pass) |

- **`boot`'s `udid` is not its `id`.** The id is a simctl UDID or an **AVD name**; the udid is the
  serial the emulator was allocated at boot. Only the udid may reach `appium:udid`.
- **No `hub_token`.** `Authorization` is stripped by `appiumTransport`: the hub is a loopback
  process with no credential, and the control plane does not forward the header.
- **The lease stays Appium's.** `Fleet` holds no mutex, queue or registry — it tries devices until
  one ACCEPTS a session, so a busy one comes back as the hub's own 4xx → `errDeviceBusy` →
  `ResourceBlock{mobile_device}`. Already-up devices first; **at most one cold boot per acquire**.
- **Three absences, three outcomes.** No Mac → `ResourceRunnerNotAttached` (the existing park and
  sweeper). No Appium / no Android SDK → the Mac's own capability `detail`, a tool error, no park.
  No assignee → a refusal naming the missing assignment. `interceptForwardRefusal` is what keeps a
  hop's 409 from reaching `classifyError` as a busy device.
- `Fleet` is keyed by (tenant, member, run), so nothing is process-wide; `Pool` stays unwired in
  the cloud for the reason it always was.

## One server, every tenant (migrations 114, 115)

A tenant WAS a database (`team_<uid>`) behind a pod of its own. It is now a
`tenant_id` column and a row-level-security policy in one shared database, read
by one shared Deployment.

| Piece | Where | Rule |
|---|---|---|
| identity in | `adapter/http/middleware_tenant.go` | signed `X-Internal-Tenant` / `X-Internal-Role` / `X-Internal-Actor`; **no header ⇒ 401**, never a default tenant |
| identity through | `platform/tenant` | `tenant.Identity` on the context; 469 store methods take no tenant argument |
| identity down | `adapter/store/postgres/db.go` | every statement in its own tx opening `SET LOCAL app.tenant_id`; no un-scoped path exists |
| isolation | migration 114 | 85 tables carry `tenant_id NOT NULL DEFAULT current_setting('app.tenant_id')::uuid`, `ENABLE`+`FORCE` RLS, one `tenant_isolation` policy each |
| global | `schema_migrations`, `tenants` | one schema, and the registry OF tenants — neither is a tenant's data |
| uniqueness | migration 114 | every UNIQUE key re-cut to lead with `tenant_id`; single-row tables (`board_settings`, `billing_plan`, `agent_cli_connection`) became single-row-per-tenant |
| per-tenant seed | `application/tenantboot` | migrations no longer seed; the first request seeds the board and mirrors the caller into `tenant_members` — unless the request carries `X-Internal-Scope: control`, which names no human (see `.ai/projects.md`) |
| background sweeps | `tenant.EachTenant` / `tenant.Sweep` | one tick per tenant; with no lister (self-hosted) one tick, unchanged |
| board runs | `board.RunJob.Tenant` | the queue severs request and worker, so the tenant rides the job |
| CI pipelines | `board.pipelineJob.Tenant` | same queue, same fix; without it every run logged `get task pipeline: tenant: no tenant in context` and failed the pipeline |
| roles | `adapter/http/middleware_role.go` | mutations of `/admin`, `/v1/settings`, `/v1/board/`, `/v1/llm/`, `/v1/projects`, `/v1/store/credentials`, repository config and agent subscriptions need admin/owner; reads and board work are open to member |

The application role must be **neither SUPERUSER nor BYPASSRLS**: both ignore
policies unconditionally, `FORCE` included. `TenantIsolationSuite` connects as an
unprivileged role for exactly that reason.

The `TENANT_UID` pin is gone (there is no second pod to impersonate) and the
GitHub webhook URL now carries `?t=<tenant uuid>` read off the registering
request rather than off a pod-wide env var.

### RLS does not reach the disk: the tenant is in the path

One `ReadWriteOnce` PVC serves every customer, so a directory listing on the
shared workspace root saw every tenant's directories while the query that said
what to keep saw one tenant's rows. The difference was deleted, or adopted. All
of it is now derived from `workspace.TenantRoot` — `<root>/tenants/<tenant-id>/…`,
see `.ai/workspace.md` for the table — which refuses a context with no identity
instead of falling back to the shared root.

| Was | Consequence | Now |
|---|---|---|
| `WorkspaceReaper` listed `<root>` per tenant | deleted every OTHER tenant's checkouts older than 48h, hourly | listing rooted at the tenant subtree; a foreign directory never reaches a delete branch |
| `repos/<name>` had no tenant, and 114 re-cut `root_path` UNIQUE to `(tenant_id, root_path)` | two customers' rows legally named one clone: cross-tenant read into the index, profile and chats, plus `git revert`+push on the victim's default branch (`deploywatch/rollback.go`) | `workspace.TenantRepoDir`; **and** all four adoption paths (import, restore, `EnsureIndexMirror`, `Runner.ensureWorkingCopy`) compare the checkout's origin with `remote_url` before acting — `repository.assertSameRepo` |
| `HostRootPath` re-anchored a foreign path onto `<root>/<name>` | the same collision, from the other direction and with no user action | re-anchors into the calling tenant's subtree; no tenant ⇒ no re-anchor |
| `allowed_roots: []` meant "any absolute path" | `POST /v1/repositories/open` indexed any readable directory on the pod | empty means the tenant's own subtree only; `allowed_roots` can only widen |

`HasLiveRunForTask` is the detail worth keeping: it is tenant-scoped, so a
foreign task's LIVE run reads as `(false, nil)` — a confident wrong answer, not
an error. The reaper's "every uncertain answer is keep" rule therefore never
fired for it. No guard can be built on a query RLS has already narrowed;
ownership has to come from the path. Pinned by
`TenantIsolationSuite.TestHasLiveRunForTaskLiesAboutAnotherTenantsRun`.

## N replicas of one server (migration 117)

`replicas: 1`+`Recreate` was a deploy outage for every tenant. Eight facts lived in Go maps; each moved into the row it describes, using the blocked-resource sweepers' claim pattern.

| Was (per process) | Now | Where |
|---|---|---|
| `Runner.activeTasks` | claim: no other live `running` row for the task | `port.RunClaim` → `claimRunSQL` |
| pool size = plan concurrency | claim: tenant's live runs < plan cap, read per run | `BillingGate.MaxConcurrency` |
| `RemoteExecutor.slots` per member | claim: live runs on that assignee < `ClaudeCode.MaxConcurrent` | `board_tasks.assignee_user_id` |
| `Reconciler.IsActive` | heartbeat, re-asserted inside the write | `FailIfStale`, `maxRunStale` |
| `Runner.cancels`, `session.runs` | the row, observed by the heartbeat | `startHeartbeat`, `watchRemoteCancel` |
| `PipelineRunner.inflight` gating effects | guarded terminal transition | `PipelineStore.ClaimTerminal` |
| webhook delivery map; `Runner.IsTaskActive` | `INSERT … ON CONFLICT DO NOTHING`; `HasLiveRunForTask` | `github_webhook_deliveries`, `WorkspaceReaper` |

The claim takes a **per-tenant `pg_advisory_xact_lock` before counting**: `FOR UPDATE SKIP LOCKED` locks the claimed row only, so under READ COMMITTED four concurrent claims each counted zero live runs and all four passed a cap of two. Found by `adapter/store/postgres/multireplica_test.go` — two pools over embedded Postgres as a `NOSUPERUSER NOBYPASSRLS` role, raced.
**Dissolved:** the restore ledger guards a directory on *this* host; `root_path` is re-anchored per host (`localizeRootPath`). **Removed:** `FailStaleRunning(0)` at pipeline boot — one pod restarting failed every pipeline its siblings were polling.

## Boot-time work has no tenant

A shared process has no tenant at boot and, on a cold pod, no tenant LIST. ~12 boot paths ran unscoped, raised `tenant.ErrNoTenant`, logged one Warn and did nothing — measured live as four tenants with **zero** `agents` rows.

| Kind | Answer | Examples |
|---|---|---|
| per-tenant seed | `tenantboot.AddStep` — once per tenant per process, on first request | mcp servers, role agents, llm providers¹, mobile devices¹ |
| fleet sweep | `tenant.Sweep` | billing period, webhook reconcile, index freshness |
| per-tenant read gating a singleton | removed; resolve per call | deployops token → `NewActionsAPIFor` |
| genuinely process-wide | says so | pgvector bootstrap (`DB.schemaPool`), `SetControlPlaneEndpoint` |
| must not run here at all | refused, with the reason | llm bootstrap from env/yaml, mobile pool — both cloud |

¹ self-hosted only: per-pod credentials and one Appium pool cannot be scoped to a tenant.
**A goroutine started from a request loses the tenant with the cancellation.** `tenant.Detach(ctx)` (`context.WithoutCancel`) replaces `context.Background()`: keep the identity, drop the deadline. Every repository import on every tenant was silently getting no project profile and no push webhook this way.

## Cloud mode refuses to boot misconfigured

`validateCloudRequirements` fails boot when `INTERNAL_AUTH_KEY`, `CONTROL_PLANE_URL` or `PUBLIC_BASE_URL` is missing or is not an absolute http(s) URL. The bar is **"its absence changes what the process IS"**, not "a feature is off": `CONTROL_PLANE_URL` unset made `RemoteWorkspaces()` false, so the pod registered filesystem tools for an empty `/data`, tried to run a `claude` binary the image does not ship, and sent embeddings to a chat provider — writing incomparable vectors into one `workspace_chunks`. One Info line, a healthy pod, every board run failing. A non-https `PUBLIC_BASE_URL` stays a **warning**: it disables the MCP callback only, and a localhost dev stack cannot have https.

## Process-wide state a request writes (the credential leak)

`llm.MultiProviderClient` held `clients[provider]` — built from a tenant's decrypted API keys — plus their default and embedding pins, as FIELDS. `llmprovider.Service` rewrote all four on every settings save, from a request path. The last tenant to save decided which key every other tenant's next call spent. A mutex would only have made it deterministic.

| Was | Now |
|---|---|
| `SetProvider`/`SetDefault`/`SetEmbedding*`/`Prune` on a shared client | **removed**; no location a request writes and another tenant reads. `llm.ProviderResolver` resolves `ResolveProviders(ctx)` per call instead |
| `ReloadFunc` pushing one tenant's entries process-wide | `InvalidateFunc` — a save says only "this tenant changed" |
| — | `runtime.tenantProviders` — cache keyed **by tenant**, `providerCacheTTL` 15s, invalidated at once on the replica that served the write |
| embedding cache key `(provider, model, text)` | `(tenant, …)` — "auto" resolves per tenant, so `""` collided two tenants' models |
| one chromium profile for the process | `Session.run` tears the browser down when the tenant changes (`handoverLocked`) — cookies, localStorage, the open page |
| MCP reload registering a tenant's servers into the process tool registry | refused inside `engine.reloadMCP` — on the resource, not on one caller |
| `POST /admin/reload` (per-**tenant** admin role) rebuilding `e.cfg`, the MCP manager and the shared browser | refused in cloud; config.yml ships in the image and cannot have changed |

The TTL is cost, not correctness: it bounds how long a tenant's OWN rotated key takes to reach a replica that did not serve the rotation, and a stale entry can only serve a tenant their own previous credential. Verified in `platform/runtime/llm_tenant_test.go` — two tenants, a tenant-scoped store, a real cipher, real HTTP servers, asserting the `Authorization` header that left the process; under a deliberately shared cache key it reports `A=6 B=0`.

## Embeddings reach the tenant's Mac, not a base URL

`domain.LLMProviderLocalRunner` (`internal/adapter/llm/runner_embed.go`) is embeddings-only and
absent from `AllLLMProviderDefinitions()` on purpose — there is no base URL a tenant could type
in, only whichever Mac the acting member has attached. `Embed` reads
`tenant.Identity.{TenantID,UserID}` off its context and calls
`POST {control-plane}/internal/runner/forward/embeddings.create` with `X-Runner-Tenant`,
`X-Runner-Member` and `X-Internal-Auth` (signed with `cfg.Cloud.InternalAuthKey`, threaded in
via `llmprovider.Service.SetControlPlane` — additive, called once at boot). A `409
runner_not_attached` becomes `domain.ErrEmbeddingRunnerNotAttached`, never a generic upstream
error. `MultiProviderClient.embedOnce` resolves "auto" (no `embedding_llm_provider` row) to this
client FIRST, ahead of the chat default, and does **not** fall through to another provider on
failure — a fallback there would silently mix a different model's vectors into one index.

`workspace_indexes.embedding_model` / `embedding_dims` (migration 116) record what an index was
built with; `domain.EmbeddingProvenanceStale(indexModel, indexDims, configuredModel,
configuredDims)` is the read-time comparison against `llmprovider.Service.ResolvedEmbedding`.
Not a stored boolean — staleness tracks the tenant's *current* setting, which moves
independently of any index row.

**Absent from the catalog ≠ absent from the UI.** The settings page derived both the
embedding picker's options and "no connected provider can produce embeddings" from
`AllLLMProviderDefinitions()`, so a tenant embedding happily on their own Mac was shown an
empty dropdown, a `SQLSTATE 22P02` (the ref fell through to an `llm_endpoints` uuid lookup)
and an instruction to connect an endpoint they do not need. Three things replace that
derivation, none of which puts `local_runner` back in the catalog:

| | |
|---|---|
| `LLMProvidersResponse.embedding_on_member_mac` | the picker's option exists — a fact about the deployment |
| `ListEmbeddingModels(local_runner)` | the pin, one model, no round trip |
| `GET /v1/llm/embedding-status` | readiness, from the Mac's own `preflight.report` |

`SetEmbedding` accepts `local_runner` (List resolves `""` to it for display, so saving what
is on screen was refused) and pins the model. `adapter/runner.EmbeddingPreflight` reads the
`lm-studio` and `embedding-model` items — two items on the Mac, two states here, because a
switched-off server is a toggle and a missing model is a download.

Both ends are wired. A pass stamps the columns from `passProvenance` — model from
`ResolvedEmbedding`, dimension **observed from a real vector** rather than guessed — and a stale
index is re-embedded whole rather than incrementally, so one index never holds two coordinate
systems. `SearchChunksByIndex` refuses a stale index with `domain.ErrIndexEmbeddingStale` (never an
empty result: a wrong ranking is indistinguishable from a right one). `embedmap` **labels** rather
than refuses — PCA would silently drop the odd-length rows and re-derive its axes from the
survivors, and a person reading a map can act on a warning. Every pre-116 index is stale until its
next pass.

## How a board run reaches a Mac

The cloud orchestrates; the assignee's Mac executes. `board_tasks.assignee_user_id` names the
machine, and every hop is `POST {CONTROL_PLANE_URL}/internal/runner/forward/<method>` with
`X-Runner-Tenant`, `X-Runner-Member` and an `X-Internal-Auth` signing that same tenant.

**`X-Runner-Member` has exactly one source: `registry.MemberUIDFromContext`.**

| Producer | Records |
|---|---|
| `board.Runner` (remote workspaces) | the task's assignee — the run is async, nobody is acting |
| `tenantMiddleware` | the signed `X-Internal-Actor`, i.e. the person asking about their own Mac |
| anything else (`tenant.Sweep`, hand-built contexts) | nothing; every reader refuses |

`tenant.Identity.UserID` is the ACTING HUMAN and is not a second source. Readers
(`llm.runnerEmbedClient`, `runner.MobileHost`, `mobile.Fleet`) consult the member uid alone: while
the embed path read the identity instead, every board run died before `claude.run` with
`embed query: … no member identity …`.

| Step | Where |
|---|---|
| transport | `adapter/runner.Client` — per-method paths, JSON in; NDJSON out for `claude.run`, JSON for the rest |
| streaming | `claude.run` events are fed line by line into the existing `parseStream`, so the activity feed fills in during the run |
| call id | chosen by the client and sent in the body, so a stop pressed before the first line still has something to name |
| cancellation | `POST .../cancel` naming the id, **then** closing the body after `DefaultCancelGrace`; a `cancelled` terminal frame while our ctx is done reports as the caller's cancellation, not a failure |
| workspace | `runner.Workspaces` → `workspace.prepare`; wired to `board.Runner.SetWorkspacePreparer` |
| execution | `claudecode.RemoteExecutor` — same stream, same gates, concurrency capped **per member** |
| no Mac (board run) | `*domain.RunnerBlock` → park on `domain.ResourceRunnerNotAttached`; `board.RunnerSweeper` probes `preflight.report` once per member every 5 min |
| no Mac (user request) | `409` + `code: runner_not_attached`, **no `Retry-After`** — `adapter/http/runner_not_attached.go`, reached from `internalError` and `badRequestErr`. Body carries `self` (the caller's own Mac vs a colleague's) and `member_uid` |
| tunnel drops mid-run | stream ends without `done` → `runner.ErrIncomplete`, a plain **failure**, never a park — a park would re-dispatch into the same fault forever |

Codes map to statuses (`bad_request` 400, `unsupported_method` 404, `not_ready` 409, `cancelled`
499, `upstream` 502, `internal` 500) but callers branch on the **code**: `not_ready` and
`runner_not_attached` share 409 and mean different things to the board.

### `POST /v1/agent-cli/claude/connect` — the probe follows the sessions

`agentcli.ProbeFunc` is chosen by `runnerClient.Configured()`, the same predicate as the executor:
locally `claudecode.Probe` runs the binary here; in the cloud `claudecode.Preflight` reads the
report the member's Mac pushed (`preflight.report`, member from `registry.MemberUIDFromContext`).
Running the local probe in the cloud asked a Linux pod for the user's CLI and blocked every new
tenant at setup step 2.

| Report | Result | HTTP |
|---|---|---|
| `claude` **and** `claude-account` `ok` | `Probe{BinaryPath, Version}` from the report | 200 |
| `claude` `missing`/`unusable` | `ErrAgentCLIBinaryMissing` + **the Mac's own remediation and command**, verbatim | 400 `agent_cli_binary_missing` |
| `claude-account` `missing`/`unusable` | `ErrAgentCLIUnauthenticated` + the same, so signed-out ≠ no-plan | 400 `agent_cli_unauthenticated` |
| either item absent | plain error naming desktop-app version skew — never a sentinel | 500 |
| no Mac / `not_ready` | `*domain.RunnerBlock`, untouched | 409 `runner_not_attached` (via `internalError`) |

The **catalog snapshot is not written** on a remote deployment and `catalog_path` comes back empty:
nobody can open a pod's disk, and the CLI that would read it is on a laptop. The path used to carry
no tenant either, so `pruneAgentDirs` deleted every other tenant's directories on the shared PVC on
each connect; `snapshotRoot` is now `<tenant-root>/agent-cli/<flavor>` (`workspace.TenantRoot`), so
that is a property of the path rather than of the `s.remote` flag alone. The counts stay real; they
are read from the database. Disconnect touches no disk on a remote deployment.

### A remote session's TaskTrooper tools

`claude.run` carries an optional `mcp` object; the runner turns it into `--mcp-config` +
`--strict-mcp-config` and deletes the file however the run ends. Absent = native tools only.

| Field | Value | Note |
|---|---|---|
| `url` | `server.public_base_url` + `/api/mcp` (`mcpserver.PublicURL`) | absolute **https** only — the runner refuses `http`, so a non-https base is dropped at boot with a warning |
| `token` | per-run bearer, 32 bytes `crypto/rand` | tenant-bound at mint, expiry `claude_code.run_timeout + 15m`, revoked by the executor's defer |
| `server_name` | `tasktrooper` | required; it is the `mcp__<name>__` prefix the model calls |

- The gateway must forward `/api/mcp` **without Firebase auth**, keep `Authorization`, and mint
  **no** identity header — the token is the identity (`mcpserver.Run.Tenant`).
- `SetLoopbackOnly(false)` on this path: the client is a laptop, so the token is the whole check.
- `initGuard` is armed when and only when `mcp` was sent; its fault outranks the cancelled call it
  causes. No public base URL ⇒ no `mcp`, guard off, one boot warning — never a failed run.

### The rest of the local invocation, remotely

Three more optional `claude.run` parameters; absent ⇒ the runner passes no flag, i.e. today's
behaviour.

| Local flag | Param | Sent | Status |
| --- | --- | --- | --- |
| `--tools` | `tools` | `domain.NativeToolsForPolicy(req.Policy)`; nil for an unrestricted policy | **closed** — without it a policy was enforced on MCP tools and decorative on Bash |
| `--effort` | `effort` | `TaskExecution.Effort` | **closed** |
| child env | `env` | `toolchain.detect`'s answer from the Mac, verbatim | **closed** — see below |
| `--setting-sources` | *(none)* | — | **closed by the runner**, unconditionally `project,local`; a rule that depended on this side asking would not be one |

### `toolchain.detect` — the repository's own pins, read where the files are

`portableTaskEnv` is **gone**, and so is the local overlay on this path. Both were one bug:
`toolchain.Detect` resolves against a directory, `WorkDir` on a remote run is relative to the Mac,
so nothing was read and only `Resolver`'s unconditional PATH rewrite survived — this pod's Linux
`PATH`. The allowlist existed to stop that reaching macOS.

| | local run | remote run |
| --- | --- | --- |
| resolved by | `toolchain.Default.Overlay(workDir)` | `POST toolchain.detect {workspace}` on the assignee's Mac |
| carried on | the context (`registry.ContextWithTaskEnv`) — tools spawn processes here | the request (`TaskExecution.Env`) — the process is there |
| translated | n/a | **never**; every name is already one `checkEnv` accepts, `GOTOOLCHAIN` is already `go1.24.3` |
| empty answer | no overlay | no `env` key — never a "system"/"latest" default |
| call fails | n/a | logged; the session runs on the Mac's defaults (the status quo), the run does not fail |

`pins` (with `exact:false` constraints) and `read` are not forwarded: `read` separates "no pin file"
from "the files say nothing recognised" and is logged for that, and resolving a constraint here
would be this side deciding what a repository meant.

## Server-side filesystem tools in the cloud

`cfg.Cloud.RemoteWorkspaces()` gates them. The repository is on a laptop; a tool acting on this
pod's empty `DATA_DIR` does not error, it reports a missing file or a failing build — which reads as
a bug in the customer's project.

| Tool | In the cloud |
|---|---|
| `read/write/edit/edit_lines/delete/move_file`, `run_terminal` | **not registered** — the session's own Read/Write/Edit/Bash run on the machine with the code |
| `get_repo_tree`, `get_symbol_skeleton` | **not registered** (`indexOnlyCodeTool`) — they walk a tree that is not here |
| `codebase_search`, `expand_symbol_context` | kept — pure index reads, content is in Postgres, **because `ToolKit.RemoteWorkspaces` turns off the unindexed-edit overlay** |
| `download_file` | **not registered** — it is a workspace WRITER; see below |
| `adapter/localdevice` (simulators) | **not wired** — replaced by `runner.MobileHost`, the same interface asked over the tunnel (see "Mobile work reaches the assignee's Mac") |
| mobile bridge / `remote_adb` | unaffected — those phones were never local |

An agent naming a retired tool gets `application/registry`'s unknown-tool error. The tool POLICIES
(`domain.WorkspaceWriteTools`, `role_tools.go`) are deliberately untouched: a policy naming an
unserved tool is already a no-op, and rewriting them would break the self-hosted install.

Two of these rows were wrong until the tenancy pass and are worth stating as corrections:

- **The two kept tools were not index-only.** Both called `code.buildOverlay`, which shells
  `git -C <session workspace> diff`/`ls-files` and returns the changed files' bytes as `Snippet`.
  `code.NewToolKit(..., cfg.Cloud.RemoteWorkspaces())` now switches it off, so the claim in
  `indexOnlyCodeTool` is true rather than aspirational. Change one without the other and the name
  becomes a lie again.
- **`download_file` was registered on `cfg.Tools.Web.Enabled` alone**, outside the workspace gate,
  and was the one tool in a cloud deployment that wrote into the workspace directory. On a board
  run its root resolved to a path on the assignee's Mac, which `filepath.Abs` anchored to the
  process's own working directory: the bytes landed on the shared PVC, unquota'd and never cleaned
  up, while the tool told the model the file was in the repository.
