# Repositories & Initiative Projects

Code repositories bind a filesystem directory to codebase indexing and the kanban board. Repository-scoped chat is removed; agents are triggered by board events instead. Every repo belongs to exactly one tenant (`tenant_id`, migration 114) and the board is global **within** it — there is no second grouping under a tenant, which is what the removed `team_id` used to be. People are a separate axis: see [Teams](#teams-migration-115) for the member roster, per-member agents and card assignees added by migration 115.

## Data Model

| Table | Purpose |
|-------|---------|
| `repositories` | `name`, `description`, `root_path`, `verify_command`/`build_command`/`test_command` (settings-editable via repo PATCH, each independently clearable) (no `team_id`) |
| `repository_projects` | Links repos to initiative projects |
| `projects` | Initiative projects: `name`, `description` (no `team_id`) |
| `workspace_indexes.repository_id` | Code index scoped to repository |
| `board_tasks` | Board tasks with `board_column` slug validated against `board_columns`, global `task_number` |

## Board Columns

Column slugs are global in `board_columns`. Default template: `backlog`, `todo`, `in_progress`, `ready_for_qa`, `in_qa`, `need_revision`, `pm_uat`, `human_uat`, `done`, `released`.

## API

| Method | Path |
|--------|------|
| GET/POST | `/v1/repositories` |
| POST | `/v1/repositories/open` |
| GET/PATCH/DELETE | `/v1/repositories/:id` |
| POST | `/v1/repositories/:id/restore` (re-clone the working copy onto this host) |
| PUT | `/v1/repositories/:id/projects` |
| GET | `/v1/repositories/:id/index/status` |
| POST | `/v1/repositories/:id/index` |
| GET/POST | `/v1/repositories/:id/tasks` |
| PATCH/DELETE | `/v1/repositories/:id/tasks/:taskId` |
| GET/POST | `/v1/repositories/:id/tasks/:taskId/comments` |
| GET | `/v1/repositories/:id/tasks/:taskId/runs` |
| GET/POST | `/v1/repositories/:id/tasks/:taskId/pipelines` |
| GET | `/v1/repositories/:id/tasks/:taskId/pipelines/:pipelineId` |
| GET | `/v1/tasks` (board tasks; released >7d are in the archive) · `/v1/tasks/released?q=` · `/v1/tasks/lookup?key=` |
| GET/POST | `/v1/projects` · GET/PATCH/DELETE `/v1/projects/:projectId` |

## Agent Workspace

Board-triggered runs use `repository.root_path` as the effective workspace for shell and code tools.

A tenant's runtime can move between hosts (cloud pod ↔ the user's paired Mac), so a recorded `root_path` may name a folder the current host does not have. The card says so (`domain.GitPresence`), and `POST /v1/repositories/:id/restore` is the way back: it clones from the recorded `remote_url` into **this** runtime's layout (`<workspace>/repos/<name>`, the same helper the GitHub import uses) and re-points `root_path` at where the code actually landed. It is explicit and one repository at a time — a clone is minutes and gigabytes, so it is never started by a read — and it refuses, without touching anything on disk, unless the folder is genuinely missing and a remote is recorded.

## Project Profile

Each repository carries a sectioned profile (`repository_profile_sections`, rendered into `repositories.profile_md` for injection and the UI). It has two halves that never mix.

**Derived** (`internal/application/repofacts`, `origin='derived'`) is collected by a parser from the working copy — language mix, manifests and their scripts, `.github/workflows` with their triggers, hosting markers (Vercel/Fly/Firebase/K8s/Terraform), migrations, test layout, git conventions (default branch, branch naming, merge style, commit style) and churn hotspots. It answers *how it ships*: a deploy workflow on `push:main` is reported as "landing a commit is the deploy", and a hosting link with no deploy workflow is reported as the provider's own git integration shipping prod on the default branch and previews on PRs. The file list comes from `git ls-files`, so ignored trees (build output, agent worktrees) never enter the facts.

**Agent** (`origin='agent'`) is the judgment half — purpose, entrypoints, conventions, invariants, change recipes, danger zones, gotchas. Written only through `update_project_profile`, and only with evidence paths that resolve in the working copy; a section whose evidence does not resolve is rejected. Derived sections are refused from the tool outright.

Refresh: import and `POST /v1/repositories/:id/profile/refresh` run the full pass; the facts are stored **before** the model runs, so a failed LLM pass still leaves an accurate stack/command/deploy picture. A push diffs each section's `source_commit` against HEAD, marks only the sections whose `source_paths` moved, and re-runs scoped to those (full rebuild only when the profile is missing or older than 7d).

Proposals: the same pass fills in repository settings it can read off the tree (repo kind, sub-projects, build/test/verify commands, deploy pipeline slots). Empty settings are applied automatically; anything that would overwrite a human's choice becomes a pending proposal with its evidence, applied or dismissed from the project settings page (`POST /v1/repositories/:id/profile/proposals/:proposalId/apply|dismiss`). Dismissals survive later refreshes.

Injection: board runs and repo-bound chats get the profile narrowed by area — on a monorepo, the running agent's role picks which layout/test/CI sections come along; always-inject sections (purpose, stack, commands, deploy, git workflow, conventions, invariants, danger zones) always do. See `internal/application/repoprofile`.

## Hosting Links

`repository_hosting_links` (migration 112) binds one (repository, area) to the provider object it lives in — a Vercel project today. Area is `''` for a single-kind repo or the sub-repo kind on a monorepo, so a frontend on Vercel and a backend elsewhere coexist. It is the "where" per area; `repository_deploy_targets` stays the "how" per environment, and the two meet once: a root-area Vercel link fills the prod target's empty address/health/vars.

| Piece | Where |
|-------|-------|
| Credential (encrypted token + default team) | `app_settings` `vercel_token` / `vercel_team_id`, `port.VercelCredentialStore` |
| API client | `internal/adapter/vercel` (`/v2/user`, `/v2/teams`, `/v9/projects`) |
| Detection + linking | `internal/application/hosting` — `.vercel/project.json`, git-link ↔ remote slug, root directory, name |
| Routes | `/v1/settings/vercel*`, `/v1/repositories/:id/hosting/*` — see api-spec |

Detection never links on its own: only one decisive candidate is reported `exact`; everything else is `ambiguous`/`none` and the settings UI asks the person, who may also record "lives elsewhere" so the area is not asked again.

## Agent Board Tools

- `list_project_tasks`
- `create_project_task`
- `move_project_task`
- `update_project_task`
- `claim_project_task`
- `add_task_comment`

Repository resolution: tools read `repository_id` from context; if absent they fall back to the first repository (`DefaultRepositoryID`) since there is no team scope.

See [Workspace](workspace.md) for the board/dispatch layer.

## Teams (migration 115)

| Table / column | Purpose |
|---|---|
| `tenants` | global registry of tenants this database has served; drives `EachTenant` and the seed gate |
| `tenant_members` | local MIRROR of the control plane's roster (uid, email, name, role), upserted from the signed headers on every request that has a human on it — never the authority for a role check |

`tenantboot.Sight` declines two kinds of caller: one with no actor, and one
carrying `X-Internal-Scope: control` (`tenant.Identity.ControlPlane`). The
control plane's own calls — billing sync, OAuth token writeback — name no
person however their actor header is filled in, and this roster is the set
`resolveAssignee` validates against, so a write to it needs a human behind it.
Signing the tenant id as the actor there gave every tenant a blank "member"
whose uid was the tenant's own uuid, assignable with a 200 (18/18 tenants
measured). Fixed on both sides: tenant-manager sends no actor, and this side
refuses the scope regardless.
| `agents.owner_user_id` | NULL = shared/team agent; a uid = that member's own |
| `board_tasks.assignee_user_id` | the PERSON a card belongs to, beside `assignee_agent_id` (the agent working it) |
| `agent_memories.owner_user_id` | copied from the memory's AGENT, so a shared agent's notes stay shared; reads are "unowned OR mine" |

Dispatch: `board.Dispatcher.resolveAgents` picks by column as before, then
`narrowToAssignee` drops agents owned by anyone but the assignee. Shared
(unowned) agents are KEPT — a team has one QA agent for everybody, and excluding
it would make an assigned card unreviewable. A solo tenant has no owners and no
assignees, so nothing narrows and no lookup is made.

### Assigning a card to a person

| Path | Contract |
|---|---|
| `POST /v1/repositories/{id}/tasks` | `assignee_user_id` is a plain string; absent/`""` = unassigned |
| `PATCH .../tasks/{taskId}` | pointer: omitted = leave alone, `""` = unassign, a uid = reassign |
| Refusal | a uid absent from `tenant_members` is a `400`, never stored — migration 115 chose a lookup over a foreign key so the message can say the roster fills in as people sign in |
| No roster wired | self-hosted / desktop / no Postgres: `SetMemberLister` is nil, any uid is taken as given |

`repository.Service` copies the field on BOTH paths and `BoardTaskStore.Update`
writes the column — neither did, so every card came back unassigned and the
runner refused every run with "no assignee, so there is no Mac to run it on".
