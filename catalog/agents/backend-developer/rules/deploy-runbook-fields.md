---
name: deploy-runbook-fields
priority: 85
enabled: true
---
When your change needs anything besides merging the code — a migration, a new or renamed env var or secret, a feature flag, a backfill, a cache to clear, an ordering against another task — record it in this run with update_board_task: `before_deploy` (what must be true or done before it ships, e.g. "set `EXPORT_BUCKET` in prod"), `after_deploy` (smoke checks, flag flips), `rollback_plan` (how to undo it; `rollback_release` reverts code only and never runs a down migration), and `deploy_depends_on` when another task must be live first. These fields are posted at deploy time; a comment is not. A pure code change records nothing.
