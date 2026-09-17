---
title: Deploy targets and recipes
description: How a repository ships to each environment, the recipe catalog, and what happens between a task reaching Done and its release going live.
---

# Deploy targets and recipes

A **deploy target** is one row per (repository, environment): which provider
it ships through, the values that provider's workflow needs, a health URL,
and whether a failed release rolls back automatically. Deploy targets live on
the repository's **Deploy targets** page (**Settings → Deploy targets**,
reached from a repository), and the cross-repository view of what is live
where is the **Deployments** page under Operations.

## Providers and the recipe catalog

Each provider is an embedded markdown recipe — YAML frontmatter plus a GitHub
Actions workflow — rendered against a target's saved values. `list_deploy_templates`
and `load_deploy_template` (see [Agent tools](tools.md)) are how an agent
reads the catalog and one recipe in full, including its required secrets, its
smoke check and its rollback command.

| Provider | Recipe | Kinds | Variables |
|---|---|---|---|
| GCP Cloud Run | `gcp-cloud-run` | backend, worker, monorepo | `gcp_project_id`, `gcp_region`, `artifact_repo`, `service_name`, `health_url` |
| GCP GKE | `gcp-gke` | backend, worker, monorepo | `gcp_project_id`, `gcp_region`, `cluster_name`, `namespace`, `deployment_name` |
| AWS ECS Fargate | `aws-ecs-fargate` | backend, worker, monorepo | `aws_region`, `ecr_repository`, `ecs_cluster`, `ecs_service`, `task_family` |
| AWS Lambda | `aws-lambda` | backend, worker | `aws_region`, `function_name`, `alias_name`, `artifact_path`, `health_url` |
| Vercel | `vercel` | frontend | `vercel_scope`, `vercel_project`, `health_url` |
| Fly.io | `fly-io` | backend, worker | `fly_app`, `health_url` |

Every recipe targets `stage`, `preprod` and `prod`. A `{{var}}` placeholder in
the rendered workflow is filled from the target's saved values; GitHub Actions'
own `${{ ... }}` expressions are left untouched. A required variable nobody
has filled in yet stays visible in the rendered workflow as
`{{key — SET THIS}}`, so a half-configured recipe is obvious rather than
silently wrong. Each recipe also carries a `rollback_hint` — the one-line
command its own rollback mechanism runs (an alias shift, a traffic split, a
`kubectl rollout undo`, an alias-URL rollback, and so on).

Mobile releases (App Store Connect and Google Play) are a separate path with
no recipe — see [Mobile devices and store releases](mobile-releases.md).

## Health URL and rollback policy

Two fields on a target matter beyond the recipe itself:

- **`health_url`** — what `get_task_deploy_status` and the production health
  monitor probe to decide whether an environment is up (see
  [Production incidents](incidents.md)). It only ever answers "is it up"; it
  says nothing about a deploy that came up but is failing a migration on
  boot.
- **`auto_rollback`** — whether a production incident attributed to this
  environment's most recent release is rolled back automatically, or only
  proposed for a human to confirm. Off is the safer default for anything
  without solid staging coverage.

`health_url` and `logs_url` are both validated by the same outbound URL guard
that covers every agent-writable address (see
[Data directory and security](data-and-security.md)) before they are saved,
and re-validated on every fetch afterward — a name that resolved to a public
address when it was saved is free to answer `127.0.0.1` later.

## The Deployments page

Operations → Deployments shows a matrix of every repository against every
environment it deploys to, refreshed roughly every 15 seconds. Selecting a
cell opens the run's detail — the same GitHub Actions run, commit status or
GitHub Deployment that `get_task_deploy_status` reads (see below) — with
rollback where the target allows it.

## Vercel account and hosting links

Settings → Integrations connects a Vercel account with a personal token,
verified against `/v2/user` before it is stored; a team can be chosen there
too, or left as the personal account. Once connected, a repository's
**Hosting** detection (`GET /v1/repositories/{id}/hosting/detect`) looks for
Vercel project markers in the working copy (`.vercel/project.json`, a git-link
match, the repository name) and proposes a candidate; only one decisive match
counts as `exact` and gets linked automatically, everything else asks you to
confirm. A confirmed Vercel hosting link on a repository's root area fills in
the empty parts of its `prod` deploy target — base URL, health URL, and the
recipe's own variables — instead of asking you to type them twice.

## Release flow

A task reaches its release only after Done, through `trigger_release`:

1. **Released column, or `trigger_release`.** The release is refused outside
   `done`/`released` — a re-release from `released` is legitimate, anything
   earlier is not.
2. **The pre-deploy checklist is posted.** The task's `before_deploy` and
   `rollback_plan` fields are posted together as one system comment on the
   card. `before_deploy` carries a generated block naming this task's release
   order — which tasks it ships after, and which it must be built after —
   derived from its `deploy_depends_on` and `blocked_by` relations and kept in
   sync whenever those change; anything you wrote by hand around that block
   survives regeneration.
3. **The deploy runs**, dispatched against the task's own merge commit (tagged
   `release/<short-sha>`), not against whatever the default branch happens to
   hold — so the release is guaranteed to be the exact commit every earlier
   gate was evaluated against.
4. **`after_deploy` is posted** once the production deploy finishes
   successfully.

A schema migration is treated specially: `has_migration` is detected from the
branch diff, and `trigger_release` refuses to release it unless the task's own
stage deploy already succeeded (`stage_verified_at` is set) — a migration is
the one class of change build and test cannot judge, since both stay green
while the rollout itself breaks production.

## Watching the deploy and rolling it back

Three agent tools carry the release from merge to production, all held by
the QA agent alone and available in Done and Released — the same tools are
stripped everywhere else, so nothing can roll production back from inside a
review column.

**`get_task_deploy_status`** answers what happened to the commit
`merge_task_pull_request` produced, from whichever of three signals the
repository actually has:

| Signal | Source |
|---|---|
| `actions_run` | the DEPLOY job inside a GitHub Actions run for that commit |
| `commit_status` | the commit status a push-to-deploy provider (`vercel[bot]`, for example) writes |
| `deployment_status` | the GitHub Deployment opened against the commit |

The answer is one of `success`, `failure`, `pending`, `no_signal` (nothing
anywhere reports a deploy of this commit — an answer, not a gap) or `unknown`.
**A `pending` result never makes the agent wait.** The task is parked instead
— the run ends, the card sits blocked, and a periodic sweep re-checks GitHub
every couple of minutes and wakes the task the moment the deploy settles, at
the cost of one API call per pass and no LLM time spent waiting.

**`get_deploy_logs`** reads the log behind a failed deploy: the Actions job's
own log, or the target's `logs_url` when the repository deploys on push with
no workflow. The result is a summary — the error-looking lines lifted out
first, then the tail — not a raw dump.

**`rollback_task_release`** undoes this task's release, only when the task
actually owns the environment's current live commit. Which mechanism runs
depends on what the repository has: a mapped deploy workflow is
re-dispatched at the last known-good tag (`workflow_dispatch`); a repository
with no workflow (push-to-deploy) instead gets a plain `git revert` of the
merge commit, pushed to the default branch — never a force-push, and a
revert that conflicts is aborted rather than resolved automatically, leaving
production unchanged and the conflict reported. Every rollback result names
the `manual_steps` a git revert cannot perform on its own — undoing a
migration, flipping a feature flag, purging a CDN — pulled from the task's
own `rollback_plan`.

When `auto_rollback` is off, nothing is executed: the proposal is written on
the card and an incident is opened for a human to confirm through the
Deployments page instead.

## See also

- [Production incidents](incidents.md) — how a failed deploy or a failing
  health check turns into an incident, and how it gets attributed back to the
  release that caused it
- [Agent tools](tools.md) — the full deploy and prod-ops tool list
- [Git and pull requests](git-and-pull-requests.md) — how a task's commit
  reaches the merge that release watches
