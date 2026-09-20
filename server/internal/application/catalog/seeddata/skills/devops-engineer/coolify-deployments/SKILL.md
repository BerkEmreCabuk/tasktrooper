---
name: coolify-deployments
category: infrastructure
description: Use when deploying or debugging an app on Coolify - git-based and Dockerfile/compose deploys, environment variables, persistent storage, health checks, domains and TLS, webhooks and the API, zero-downtime and rollback
tech_stack: Coolify
---

# Coolify Deployments

## Overview

Coolify is a self-hosted PaaS: it builds from a git repository (Nixpacks, a Dockerfile, or a compose file) and runs the result behind its own reverse proxy (Traefik by default) on a server you own.

**Core principle:** What Coolify runs comes from the repository. The UI holds two things the repository must not: the environment's secret values and the server/domain wiring. Everything else — the Dockerfile, the compose file, the health endpoint, the start command — is code, reviewed in a PR.

## Setting up an application

1. **Build pack.** Prefer an explicit `Dockerfile` (or `docker-compose.yaml`) over Nixpacks auto-detection: it is the same build locally, in CI and on the server. Nixpacks is fine for a throwaway service.
2. **Port.** Set the exposed port to the one the app actually listens on, and make the app bind `0.0.0.0` — a process bound to `127.0.0.1` inside the container is unreachable through the proxy.
3. **Health check.** Point it at a real endpoint (`/health`). Without it Coolify calls a container "running" while the app inside it is failing, and zero-downtime deploys have nothing to gate on.
4. **Domain + TLS.** Set the FQDN in Coolify; the proxy requests the certificate automatically. DNS must already resolve to the server or issuance fails — check the DNS record first (see network-tls-and-ingress).
5. **Persistent storage.** Anything that must survive a deploy (uploads, database files) is a named volume or a bind mount declared in Coolify or the compose file. A container filesystem is discarded on every deploy.
6. **Resources.** Set memory/CPU limits per service on a shared server, so one runaway app cannot take the host down.

## Environment variables

- Set values in Coolify (per environment), never in the repository. Commit a `.env.example` documenting the names and the shape of each value.
- **Build-time vs runtime:** variables the build needs must be marked as build variables, or they are absent during `docker build` and the build fails or bakes a wrong default.
- Changing a variable requires a redeploy to take effect.
- Coolify can generate service credentials (database passwords etc.); read them from the service's own variables rather than copying them around.

## Deploying

- **Automatic:** connect the repository and enable deploy-on-push for the branch, or call the deploy webhook from your own pipeline after tests pass — the second is better: it means only a green commit reaches the server.
- **From CI:** the deploy endpoint takes the application's uuid — `curl --fail -X GET "$COOLIFY_URL/api/v1/deploy?uuid=$APP_UUID" -H "Authorization: Bearer $COOLIFY_TOKEN"` — or use the application's own deploy webhook URL exactly as Coolify prints it. A bare POST with no uuid and no body deploys nothing. Store the token and the uuid as CI secrets, and check the response rather than assuming a 200.
- **Zero downtime:** enable it only with a working health check; the old container is kept until the new one is healthy. With a single container and no health check, a deploy is a short outage — say so on the card rather than claiming otherwise.
- **Rollback:** Coolify keeps previous images and has a first-class Rollback (Application → Configuration/Deployments → Rollback), which redeploys a retained image without a rebuild — prefer it, and know which deployment you would pick BEFORE you deploy. Redeploying the previous commit also works but pays for a full rebuild. Neither reverses a database migration, restores a volume or turns a feature flag back off; those steps are yours.

## Debugging

Read the deployment log in Coolify first — a failed build and a failed start look identical from outside. Then the container logs. Common causes: the app binds `127.0.0.1`; the exposed port does not match; a required environment variable is missing; the health check path 404s; the volume mount path does not match what the app writes to; the disk on the server is full (`docker system prune` is Coolify's own housekeeping, do not run it blindly on a shared host).

## Red flags

- Editing files inside a running container — the next deploy erases it.
- Secrets in the repository "because Coolify reads the repo anyway".
- A database running as a container with no volume and no backup schedule.
- The whole stack on one server with no backup of the Coolify instance itself (its own configuration is state too).
