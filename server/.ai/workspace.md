# Workspace

The system is a single implicit workspace. The team layer was removed (migration 038). Board columns, membership, column subscriptions, and the task key prefix are global; every profile/agent works in the same board.

## Data Model

| Table | Purpose |
|-------|---------|
| `board_settings` | Single row (`id=1`): legacy `key_prefix`; task keys no longer use it (see below) |
| `board_task_counters` | One row per task type: `last_number`, monotonic — task numbers are never reused |
| `board_columns` | Global column slugs, labels, order, backlog flag |
| `board_members` | Catalog agents active on the board |
| `agent_column_subscriptions` | Which agents listen to which column slugs (with optional `task_type_filter`) |
| `repositories` | Code repos bound to the board (no `team_id`) |
| `board_tasks` | Board tasks; `task_number` unique per task type (key = `T-`/`B-`/`A-` + number), `board_column` slug validated against `board_columns` |
| `task_comments` | User and agent comments on tasks |
| `board_events` | Audit trail for task lifecycle events |
| `task_agent_runs` | Headless agent executions triggered by board events |
| `sessions.agent_id` | Direct agent chats scoped by `agent_id` (optional `repository_id`) |
| `session_actions` | Ledger of board records a chat's agents created or changed (063) |

## Agent Chats

User-initiated chats are scoped by `agent_id` (optionally `repository_id` so board tools work in context). `POST /v1/sessions` with `agent_id`; `GET /v1/sessions?agent_id=`. When `agent_id` is set, `session.Service` loads catalog agent prompt, skills, rules, and tool policy (orchestration disabled). The agent must be a board member.

### Session action ledger

An agent loop's tool calls and results live only inside that loop — only the final assistant text is persisted. A later turn therefore had no id for the task it had just created, and re-created it instead of moving it.

`registry.ActionRecordingRegistry` (wrapping the audited registry, so chat, orchestrated subtasks and board runs are all covered) writes every state-changing board tool call into `session_actions`: tool, verb, entity kind, entity id/key, title, column, priority, full result payload. Read-only tools are not recorded. Classification and the prompt rendering live in `domain.ClassifyBoardAction` / `domain.SessionActionDigest`.

Two consumers:

- `session.Service.buildMessageHistory` injects the digest as a system message directly ahead of the newest user turn, so the agent addresses existing records by id. `isolatedSubtaskHistory` preserves this one system message (`domain.IsSessionActionDigest`) while still stripping session-level prompts.
- `GET /v1/sessions/:id` returns an `actions` array; the web chat renders each as a `SessionActionCard` under the reply that produced it, opening the board's `TaskDetailDrawer` via `ChatTaskDrawer`.

Persisted clarifications are also replayed into history (`prompt.ClarificationHistoryNote`) — the questions previously lived only in a JSONB column the model never saw, so it read the user's answer without knowing what had been asked.

## Dispatch Rules

| Event | Agents triggered |
|-------|------------------|
| `task.created` in column X | Subscribers of column X |
| `task.moved` to column X | Subscribers of column X |
| `task.commented` + assignee + `need_revision` | Assignee only |
| `task.commented` (other) | Assignee if set, else column subscribers |
| `task.assigned` | New assignee |

Dispatcher writes `board_events`, creates `task_agent_runs`, and enqueues `board.Runner` jobs. Column subscriptions are resolved from `agent_column_subscriptions` (global) via `BoardConfigStore.AgentsForColumn`.

## On-disk layout under `storage.sessions.workspace_root`

One process, one PVC, every customer. Row-level security does not reach a
filesystem path, so the tenant is IN the path — `workspace.TenantRoot` is the
only way any of these is derived, and it refuses a context with no identity
rather than falling back to the shared root.

| Path | What |
|---|---|
| `<root>/tenants/<tenant-id>/repos/<name>` | index mirror clone, one per repository (`workspace.TenantRepoDir`) |
| `<root>/tenants/<tenant-id>/task-<task-id>` | a task's own checkout (`workspace.TenantTaskDir`) |
| `<root>/tenants/<tenant-id>/<session-id>` | an unbound chat's scratch dir (`workspace.SessionDir`) |
| `<root>/tenants/<tenant-id>/agents/<agent-id>` | an agent chat's persistent scratch dir (`workspace.AgentDir`) |
| `<root>/tenants/<tenant-id>/agent-cli/<flavor>/<agent-id>` | agent-CLI catalog snapshot (local deployments only) |

`repos/<name>` is the only non-uuid component, which is why the tenant segment
above it is load-bearing: migration 114 re-cut `repositories`' unique key to
`(tenant_id, root_path)`, so two customers with a repository called `api`
became two rows legally naming one directory.

**Existing volumes.** `<root>/repos/<name>` written under the flat layout is
left where it is and keeps working — its row names it, and `UsableHostPath`
returns any path that exists unchanged. `GetByRootPath`'s directory-name
fallback recognises that shape (`hostRoots.preTenantLayout`) so a re-import
finds the existing row instead of inserting a second one beside it. Nothing new
can be created there:
`ValidateProjectRoot`, the import, the restore and `Create` all derive from the
tenant subtree now. Flat `<root>/task-<uuid>` directories are different — they
hold uncommitted work — and the reaper MOVES them into the owning tenant's
subtree on its next pass, but only on proven ownership (the task uuid is in
that tenant's own RLS-filtered list; task uuids cannot collide across tenants).
Anything it cannot prove is left untouched, never deleted.

## Headless Agent Runs

`board.Runner` loads catalog agent config, creates an ephemeral session for activity tracking, injects repository workspace and index context, and runs `agent.Loop` with a board trigger message. Agents use `claim_project_task`, `add_task_comment`, and existing board tools.

### Working copy is a cache, not the source of truth

`repositories.remote_url` (064) records the git origin. The clone under `<workspace_root>/tenants/<tenant-id>/repos/<name>` can disappear (fresh pod, wiped disk) and is restored from that URL.

Before any agent starts, `board.Runner.ensureWorkingCopy` requires a real git working copy at the repo root **that is this repository**: intact and its origin matches `remote_url` → use it; intact but a different origin → **fail the run** (adopting a checkout is only safe if it is the right one); missing/empty → clone from `remote_url`; no `remote_url`, or the root exists with non-git contents → **fail the run** with an operator-facing error. It previously called `os.MkdirAll` here, so a stale path silently became an empty directory, `HasGit` went false, the clone/branch gate below was skipped, and the agent ran in an empty tree and asked the human for the repository path.

`remote_url` is written on import/open and backfilled from `git remote get-url origin` while a clone is still present (`repository.Service.syncRemoteURL`) — rows predating the column carry `''`.

### `repositories.root_path` is advisory across hosts

One tenant database can be served by **two hosts running the same binary**: the GKE pod (PVC mounted at `/data`) and the user's own machine behind a reverse tunnel, where `DATA_DIR` is e.g. `<repo>/local-runner/data` and `/data` cannot exist at all (the macOS root filesystem is read-only). `root_path` is an absolute path belonging to whichever host wrote the row, so a board run on the Mac read the pod's path and died in `git clone` with `mkdir /data: read-only file system`.

**Reads translate; writes stay host-absolute.** `postgres.RepositoryStore` re-anchors a foreign path onto the reading host, so `root_path` is a hint about *this* host's filesystem, never a cross-host address:

| Stored path, as seen by the reading host | Result |
|---|---|
| Exists here | used unchanged — a self-hosted user may point a repo anywhere |
| Under this **tenant's own subtree** or an `indexer.allowed_roots` entry (even if not yet cloned) | used unchanged — this host may still create it |
| Anything else (foreign) | final path segment re-anchored under this **tenant's** subtree, logged once at INFO with both paths |
| Anything, with no tenant on the context | used unchanged — there is no destination that could be right |

The rule lives in `workspace.HostRootPath` / `workspace.UsableHostPath`; the store applies it in `localizeRootPath`, which every `repositories` scan runs through (`scanRepository`), so the board runner, task chat, webhooks, repo profile, indexer and board tools all get a usable path without knowing the rule exists. `runtime.go` supplies this host's roots via `NewRepositoryStore(pool).SetHostRoots(cfg.Storage.Sessions.WorkspaceRoot, cfg.Indexer.AllowedRoots)`; a store with no host roots (tests) is the old pass-through.

It is **symmetric** — the pod translates a Mac-written path the same way — which is what makes writing a plain host-absolute path harmless. `repository.Service.Open` therefore keeps storing its own `absRoot` (validated by `workspace.ValidateProjectRoot`, so it must exist on the writing host) and never rewrites an existing row's `root_path`.

`GetByRootPath` follows: exact match first, then a match on the directory name alone, restricted to rows whose stored path is foreign to this host **or names this host's pre-tenant layout** (`hostRoots.preTenantLayout`). Without that, re-opening the same repository from the second host inserts a second row for one remote and splits the tasks, indexes and runs hanging off the repository id. Two *local* repositories that merely share a directory name are never folded together.

### The session and index paths follow the same rule

`sessions.workspace_dir`, `sessions.project_root` and `workspace_indexes.root_path` are the same bug class and are now translated the same way, by the same helper. `postgres.SessionStore` and `postgres.IndexStore` each gained `SetHostRoots` and a `localize*` applied to every scan (`scanSession` covers Create/Get/FindByTask/List*; the index store covers its three creates plus `scanIndex` and `GetIndexByProjectBranch`), wired in `runtime.go` from the same `cfg.Storage.Sessions.WorkspaceRoot` + `cfg.Indexer.AllowedRoots` pair. The shared rule and the log-once bookkeeping live in one type, `postgres.hostRoots`, which `RepositoryStore` also uses.

- **`sessions.workspace_dir`** is the directory a turn runs in: a repository root, a task checkout `<tenant-root>/task-<id>`, or the chat's own scratch dir `<tenant-root>/<session-id>`. Resuming a pod-written chat on the Mac used to reach `os.MkdirAll("/data/workspaces/…")` in `session.Service.ensureSessionWorkspace` and fail the turn. Because the re-anchor lands `task-<id>` and `<session-id>` directly under this tenant's subtree — exactly the paths this host derives itself — a resumed task chat now agrees with `board.TaskPRService.TaskWorkspacePath` **without rewriting the column** — the write at `ensureSessionWorkspace` only fires when the paths really differ. (`workspace.AgentDir` is `<tenant-root>/agents/<agent-id>`, so a foreign agent-chat dir re-anchors to `<tenant-root>/repos/<agent-id>` instead; harmless and stable — it is scratch space.)
- **`sessions.project_root`** is the subtree the index endpoints and code tools are scoped to, and travels with `workspace_dir`.
- **`workspace_indexes.root_path` is re-anchored, not invalidated.** Everything derived from the tree is stored *relative* to that root — `workspace_symbols.file_path`, `workspace_chunks.file_path` and `workspace_file_hashes.file_path` are all `filepath.Rel` results, and a chunk carries its own text in the row — so the index body is host independent and moving the anchor cannot make a stored chunk describe a file it did not come from. After a read, `root_path` reaches the filesystem in exactly one place: `indexer.Injector` rendering the code skeleton via `mapper.BuildSkeletonRanked`, and only when the run context carries no workspace dir of its own (a live workspace already wins at `inject.go`). That walk reads the *current* tree, so anchoring it here describes this host's checkout; leaving it foreign makes the walk fail and the error is swallowed, so the agent silently loses the skeleton section instead of getting a usable one. Invalidate-and-rebuild was rejected as far more destructive: it would discard every embedding for a repository on a condition that flips each time the tenant changes host, so the two hosts would take turns re-embedding the same tree forever — real provider spend and minutes of latency — to correct a staleness the hash-incremental pass (`workspace_file_hashes` → `DeleteFileData`/`SaveFileHashes`) already fixes file by file on the next pass. The staleness that remains, chunks from the other host's commit, is what a single host already lives with between index passes.

Writes stay host-absolute here too: `ensureSessionWorkspace` and `indexer.Service` (via `UpdateIndexTree`) stamp this host's own path, so a row converges on whoever ran last while the read-time translation keeps the other host safe in the meantime.

Still host-absolute and **not** translated: `task_agent_runs.workspace_path`, which is only ever written (`board.Runner`, from this host's workspace root). Every consumer that needs a task's checkout derives it locally — `board.TaskPRService.TaskWorkspacePath`, `repository.Service.taskWorkspacePath` — and nothing reads the column back for filesystem use. It is still returned on the `/runs` API, so the board can display the path of a run that happened on the other host; that is cosmetic.

### Per-task git workspace

When the repo root is a git repository, the runner clones it into `<workspace_root>/tenants/<tenant-id>/task-<taskID>/`, checks out `feature/<task-key>` (e.g. `feature/t-12`, `feature/b-3`), and uses that clone as the run's effective workspace (`task_agent_runs.workspace_path`). On success it commits and pushes the branch. A re-run reuses the existing workspace/branch. On workspace setup failure, falls back to the repo root.

`EnsureTaskWorkspace` fetches the project root and then the fresh clone before branching, and cuts the task branch from `origin/<default branch>` — otherwise every task branched off whatever stale state the shared root happened to hold. A fetch failure is not fatal (offline runs still work) but is logged and drops the branch back to the cloned HEAD.

When a task moves to `pm_uat` or `done`, `repository.Service` creates a draft PR via `gh pr create --fill --draft` (idempotent) and appends the PR URL as a system comment.

### Clarification notifications

If a board run's response contains a clarification request (`ask_user`), the runner opens a new agent chat session (agent + repository bound) with the clarification stored on the assistant message, and sends a macOS notification via `osascript`.

## API

| Method | Path |
|--------|------|
| GET | `/v1/board/config` (settings + columns + members + subscriptions) |
| GET/PUT | `/v1/board/settings` (key_prefix) |
| GET/PUT | `/v1/board/columns` |
| GET/PUT | `/v1/board/members` |
| GET/PUT | `/v1/board/subscriptions` |
| GET | `/v1/activity` |
| GET | `/v1/tasks` (board tasks — released older than `domain.ReleasedBoardWindow` = 7d excluded) |
| GET | `/v1/tasks/released?q=&limit=` (released archive: all of them, newest first, search over key/title/description) |
| GET | `/v1/tasks/lookup?key=` |
| GET/POST | `/v1/repositories`, `/v1/repositories/open` |
| GET/POST | `/v1/projects` (initiative projects) |
| GET/PATCH/DELETE | `/v1/projects/:projectId` |

Task comments and runs: `/v1/repositories/:id/tasks/:taskId/comments`, `/runs`.

## Config

```yaml
board:
  dispatch_enabled: true
  max_concurrent_runs: 3
```

## Agent performance (global)

Migration 038 reverted team-scoped scoring: `agent_performance_scores` is UNIQUE(agent_id); `agent_score_events`, `agent_memories`, `agent_reflections`, `agent_evolution_events`, `agent_kpi_results` are agent-global (no `team_id`). Prometheus: `bridge_agent_score{agent_id}`.

UI: `/agents/:agentId/performance` shows score + trend sparkline, KPI attainment cards + composite, evolution timeline (impact badges, before/after diff), reflections, memories (deletable), score event table, and a "Şimdi analiz et" button.

API: `GET /v1/agents/:agentId/performance|score-events|evolution-events|reflections|memories`, `POST .../reflect` (202; 409 while in flight), `DELETE .../memories/:memoryId`.

Memory listings (`/v1/agents/:agentId/memories`, `/v1/memories/shared`) take `repository_id` + `repo_scope` and creates take `repository_id` — see [Memory scopes](orchestration-agents.md#memory-scopes-migration-065). Memories are agent-global only in the sense that there is no team layer; the repository dimension is migration 065.

## Where a run's workspace lives (cloud vs self-hosted)

`board.Runner.remoteWorkspaces()` — set when `CONTROL_PLANE_URL` is configured — picks between two
true lifecycles. Everything above this section describes the local one, unchanged.

| Step | local (self-hosted, desktop) | remote (shared cloud) |
|---|---|---|
| working copy | `ensureWorkingCopy` clones `remote_url` into `root_path` here | `workspace.prepare` on the assignee's Mac (`adapter/runner`) |
| task checkout | `EnsureTaskWorkspace` → `<workspace_root>/task-<id>` | `<repo>/task-<id>` under the Mac's workspace root; only the returned `rel` is ever passed on |
| task branch | cut here from `origin/<default>` | **not** cut here — the Mac clones and checks out only; the session cuts `tt-<key>` itself, so run 1 gets the default branch and run 2 finds the branch on origin |
| `run.workspace_path` | this host's absolute path | the **Mac's** absolute path — recorded for a human, read by nothing |
| grounding / verify / commit / push / PR | cloud-side git steps | all inside the Claude Code session, which is the only process holding the tree |
| agent catalog (`agentfs`) | materialised into the checkout | skipped — skills go in the prompt (`prompt.SkillsInPrompt`), as for every non-CLI provider |
| no Mac attached | cannot happen | park on `domain.ResourceRunnerNotAttached`, released by `board.RunnerSweeper` |

The **index mirror is separate and stays in the cloud**: `repository.Service.EnsureIndexMirror`
restores `root_path` from `remote_url` before a pass, because that clone is a cache on an ephemeral
pod disk. It is only ever read by the indexer.
