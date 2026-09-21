---
name: cloud-deploy-gcp-aws
category: deployment
description: Use when a backend or worker service needs a cloud deploy - container-first GitHub Actions deploys to Google Cloud Run (WIF) or AWS App Runner/ECS (OIDC), with migrate-before-deploy and a health-check gate
---
# Cloud Deploy — GCP & AWS (backend/worker)

## Overview

A backend deploy ships the **container the repo already builds**, not a re-implementation. The same image runs on Google Cloud Run and on AWS App Runner/ECS — the deploy workflow only differs in the auth handshake and the deploy command. Read [[ci-cd-pipeline-authoring]] first: it defines the workflow naming/dispatch contract this skill fills in with real cloud steps.

**Core principle:** One image, one health-checked deploy per environment, authenticated by OIDC — never a hand-rolled server or a committed key.

## Pick the target, then copy the boilerplate

1. Decide the cloud (project's existing infra wins) and app type (`backend` or `worker`).
2. `search_boilerplate_catalog` → `deploy gcp backend`, `deploy aws worker`, etc. Copy that folder's `deploy-stage/preprod/prod.yml` into the project's `.github/workflows/` as `<id>-deploy-<env>.yml`.
3. Fill in the repo/environment **vars** its README lists (project id, region, service name, image repo). Set up the OIDC trust once (WIF pool / IAM role) — the README has the exact steps.

## GCP — Cloud Run

- Auth: `google-github-actions/auth` with `workload_identity_provider` + `service_account` (no key JSON).
- Deploy: `gcloud run deploy $SERVICE --image $IMAGE --region $REGION` (or `--source .` to build on deploy). Cloud Run gives you the revision URL to smoke-test.
- Worker (no HTTP ingress): deploy as a Cloud Run **job** (`gcloud run jobs deploy ... && gcloud run jobs execute`) or a GKE workload — not a public service.

## AWS — App Runner / ECS

- Auth: `aws-actions/configure-aws-credentials` with `role-to-assume` (OIDC), no access keys.
- **App Runner** (simplest): push the image to ECR, then `aws apprunner start-deployment` (or update the service). Good default for a single service.
- **ECS Fargate** (more control): push to ECR, render the task definition, `aws-actions/amazon-ecs-deploy-task-definition` with `wait-for-service-stability: true`.
- Worker: an ECS service with no load balancer / no public subnet.

## Migrate before deploy

- Schema migrations run as a **pre-deploy step against the target environment's database**, gated per environment, before the new image is switched in. A failed migration must fail the workflow before any traffic shift.
- Migrations are forward-only and backward-compatible with the currently-running image (expand/contract) so a rollback of the image doesn't break on the new schema. See [[postgres-migrations]].
- Never point a stage/preprod deploy at the production database. One database per environment; the DB URL comes from that environment's vars/secrets.

## Health-check gate & rollback

- End every deploy job with a smoke step: curl the health endpoint (Cloud Run revision URL / App Runner service URL / ALB DNS) and **fail the job on non-200**. That exit code is what moves the task to `released` vs `need_revision`.
- Rollback is redeploying the previous image tag/revision — keep deploys immutable-tagged (git SHA), never `:latest`. Put the rollback command in the task's `rollback_plan` field (`update_board_task`) so QA reads it when a prod deploy goes bad — that field is posted on the card automatically at deploy time, which a comment is not.

## Common Mistakes

- Deploying `:latest` → no deterministic rollback target. Tag with the commit SHA.
- Running migrations from inside app startup instead of a gated pre-deploy step → half-migrated env on crash-loop.
- Stage deploy sharing the prod database.
- A deploy job that reports success without ever hitting the health endpoint.
- Storing a GCP service-account JSON or AWS access key in `secrets` instead of using OIDC/WIF.

## Red Flags

- The workflow has no `id-token: write` permission (OIDC can't work).
- No smoke step, or a smoke step whose failure doesn't fail the job.
- Preprod/prod deploy triggers on `push` instead of `workflow_dispatch`.
