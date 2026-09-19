---
name: release-and-rollback
category: ci-cd
description: Use when shipping a change to an environment - environment promotion, versioning and tagging, deploy strategies (rolling, blue-green, canary), database migration ordering, post-deploy verification, and the rollback plan
---

# Releases and Rollbacks

## Overview

**Core principle:** A release is not finished when the deploy command exits. It is finished when the new version is verified healthy and you can name the exact way back. Decide the way back BEFORE deploying.

## Promotion

`local → stage → (preprod) → prod`, with the same artifact moving up: build the image once, promote that digest. Rebuilding per environment means the thing you tested is not the thing you shipped. Environment differences live in configuration, never in the build.

Check `get_deploy_target` for the environment's address and health URL, and `get_pipeline_status` for the state of the build you are about to promote. A red pipeline is not promoted — not with an override, not "because the failure is unrelated".

## Versioning

Tag the commit with the version you deploy (`v1.4.2`) and put that version into the artifact and the running app (a `/version` endpoint or a build label). When something breaks at 3am, "which commit is live?" must be answerable in one request.

## Strategies

| Strategy | When | Cost |
|---|---|---|
| Rolling | The default for stateless services with health checks | Two versions run briefly — the change must be backward compatible |
| Blue-green | Cutover must be instant and reversible | Double the resources during the switch |
| Canary | Risky change, enough traffic to measure | Needs routing control and per-version metrics |
| Recreate | Single-instance apps, or when two versions cannot coexist | A short outage — say so beforehand |

Any of them plus a feature flag lets you ship the code dark and turn the behaviour on separately, which is the cheapest rollback there is.

## Database migrations

Migrations are forward-only in practice: a rollback of the code must work against the already-migrated schema. So expand, then contract, across two releases:

1. **Expand:** add the column/table, nullable or defaulted; deploy code that writes both and reads the old.
2. Backfill, then deploy code that reads the new.
3. **Contract:** in a LATER release, drop the old column.

Never ship a schema change and a destructive cleanup in the same release, never rename in place, and never make a migration the deploy cannot survive being run twice.

## Post-deploy verification

In the same run: health endpoint green, the version endpoint reporting the new version, one real request through the actual entry point, error rate and latency compared to before, logs read for new exception types. Then record it (`record_local_deploy` for a local/manual deploy) so the board knows what is where.

## Rollback

Write it down before deploying, as a command: `kubectl rollout undo deploy/x`, Coolify's Rollback to the previous deployment, repoint blue-green, or redeploy the previous image **by digest** (`@sha256:…`) or its immutable version tag — never by moving a tag, which makes the rollback target mutable and destroys the record of what was live. Rolling back is a normal operation, not an admission of failure — the incident is prolonged by debating it. When a rollback is impossible (a contracted migration, an external side effect), that is a fact the task must state before it ships, not after.

## Red flags

- A deploy whose only verification is that the pipeline turned green.
- A hotfix applied directly to the server, so the next deploy silently reverts it.
- A release with no tag, so nobody can say what was shipped.
- "We'll roll forward" as a plan with no estimate of how long forward takes.
