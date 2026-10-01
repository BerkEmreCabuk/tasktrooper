---
name: post-deploy-verification
category: release
description: Use when a release reaches awaiting_verdict (or you are woken for an incident) - how to read runtime logs, error groups, health and smoke evidence for the release's component and decide finish, re-check, roll back or hand to a human
source: obra/superpowers (MIT), adapted
---

# Post-deploy verification

A deploy that went green tells you the process started. It says nothing about whether the change it shipped actually works, or whether it broke something else that happened to keep running. Verification is what closes that gap, and it happens every time — not only when something looks wrong.

## 1. Procedure — exact calls

1. `get_release({ task_id })` → read `deployed_at`, `component`, `checks` (health samples, smoke results, `new_errors`, `notes`, `early_stop`), `profile.auto_rollback`, and `tasks[]` — every task this release covers, not only the one you were woken on.
2. `get_environment` only if you need to confirm nothing is bound (`checks.notes` already says so in most cases).
3. `list_runtime_errors({ component, since: "<window that starts well before deployed_at>" })` — see "Reading error groups" below for the window and how to judge `new`.
4. `query_runtime_logs({ component, since: deployed_at, min_severity: "error" })`; re-query with `text:` set to a route or module the release changed when the error list alone is not enough.
5. `run_smoke_checks({ task_id })` when the stored evidence is older than ~30 minutes, or when `early_stop` itself came from a smoke failure and you need a fresh read before deciding.
6. One read-only acceptance check per observable criterion of **every** task in `release.tasks` (`list_acceptance_criteria` per task) — a release with several tasks is verified in full, not just the card that woke you.

## 2. Reading error groups

`list_runtime_errors`' `new: true` is accurate on a provider with native grouping (gcloud). On a provider without one (Vercel, AWS — fallback fingerprint grouping), it only means "this group's first occurrence fell in the back half of whatever window you queried" — querying `since: deployed_at` puts a regression that starts right after the deploy in the window's *front* half, so it can read `new: false` and never show up as a concern.

- Judge a group by its own `first_seen` against `release.deployed_at`, never by `new` alone.
- Read with a window that starts well before `deployed_at` — roughly twice the time elapsed since the deploy, or at least the soak period — so an occurrence from before the deploy is visible and can be told apart from one that started with it.
- Before rolling back on one low-count group, re-read with `since: "1d"`. If the same message recurs before `deployed_at`, it is chronic, not a regression. Once you have confirmed a group is chronic and unrelated to any release, `save_memory` (project scope) a one-line note of its fingerprint message ("`redis timeout` in apps/api recurs hourly; not a release signal") so a future run of this skill recognizes it without a full 1-day re-read.
- `checks.new_errors` (the sweeper's own field) can be empty on Vercel or AWS even when errors genuinely began with the deploy — your own read in this run is the check, not a formality that confirms what the sweeper already decided.

## 3. Verdict table

Evidence maps to one of four outcomes — finish, re-check once, roll back, or hand to a human — the same shape as Argo Rollouts' Successful/Failed/Inconclusive and Flagger's "N failed checks before rollback": one blip is not a trend, and an unreadable signal is not a pass.

| Evidence | Verdict |
|---|---|
| `early_stop: smoke check failed` | Re-run `run_smoke_checks` once. Passes, and no error group has `first_seen >= deployed_at` → finish, naming the transient failure and the re-check. Fails again → `rollback_release` (`verify_failed`). |
| Health check failed twice | `fetch_url` the `checks.health_url` once (GET). Still failing → roll back. Recovered → look for a restart/crash line in the logs since `deployed_at`; clean → finish, noting the blip. |
| An error group with `first_seen >= deployed_at` whose message, path or stack touches a file or route this release's tasks changed (`get_task_pull_request({ task_id, include_diff: false })` → files) | Roll back (`verify_failed`). |
| Same, but unrelated and it also recurs before the deploy in a `1d` read | Finish, naming each group and why it is not a regression. |
| Only structural gaps in `notes` (no bound environment, no health URL, no base URL) | Finish, stating the gap verbatim — it is a different kind of verification, not a failed one. |
| A runtime tool itself errored | Retry once. Still failing, and health and smoke are both green → finish, saying the logs were unreadable this run. |
| **Nothing at all is readable** for an `on_merge`/`dispatch` service (no health, no smoke, no logs, and the retry above also failed) | One comment: "verdict needs a human: no evidence was readable (`<what you tried>`)" — and stop. A human finishes or rolls back from the Deploy tab. This is the one case where "finish with the gap stated" is not the default: with nothing readable there is no evidence to finish *on*. |

### Recording a missing deploy target

A `notes` gap like "no health URL" or "no base URL" does not always mean nobody can fix it — if the deploy's own output (`get_deploy_logs`, a `local_run.tail`, or a workflow log) names the real URL the service answers on, `update_deploy_target({ env, base_url / health_url / logs_url })` records it so the *next* soak and verdict actually have something to read, instead of hitting the same gap every release. Still finish or roll back on the evidence you have in THIS run — recording the target fixes the next one, it does not retroactively create evidence for this one.

## 4. Feature-flagged changes

If a task's `after_deploy` (on `release.tasks[]`) enables a flag or config, its new behaviour is not reachable yet — that is expected, not a failure. Check that the old path still works, and say "criterion X is behind flag Y, enabled after release by a human" in your note. A flag flip is a manual step for a human, never something you do yourself.

## 5. Notes that count as evidence

✅ "query_runtime_logs (apps/api, since 2026-10-01T10:02Z, ≥error): 0 entries; list_runtime_errors 40m: 2 groups, both first_seen before deploy (chronic `redis timeout`, 1d read shows it hourly); smoke 3/3 (re-run 10:41); T-12 GET /api/orders?status=paid → 200 with `paid_at` field."

❌ "Looks fine, logs clean."

## 6. Rationalizations (adapted from obra/superpowers)

- "Deploy went green" → that is the process starting, not the behaviour it shipped.
- "Soak was green" → read it yourself, in THIS run — the sweeper's own fields are a starting point, not a substitute.
- "`new_errors` is empty" → on Vercel or AWS that can miss an error that genuinely started with the deploy; read `first_seen` yourself.
- "The error is probably old" → show the `first_seen`, don't assert it.
- "One failed probe" → re-check once, then decide — never roll back, and never finish, on a single sample.

## Common Mistakes

- Omitting `component` in a monorepo — silently reads the root component's logs, not the release's own.
- Reading errors with the default 24h window instead of a window anchored before `deployed_at`.
- Judging novelty by `new` instead of `first_seen` against `deployed_at`.
- Verifying only the task you were woken on when the release carries several.
- Writing to production (seeding, creating records) to check a criterion.
- "Should", "probably", "seems" anywhere in a finish/rollback note.

## Batch releases with no runtime environment

Most desktop and mobile components have no bound runtime environment at all — there is no host to sample health from, no log stream to query, no error groups to list. That is not an incomplete verification, it is a different one: see `batch-artifact-verification` for what to check instead (the published artifact, store build, or local run), and say explicitly in the `finish_release`/`rollback_release` note that no runtime environment is bound — the same rule as any other gap: never let silence read as a pass.
