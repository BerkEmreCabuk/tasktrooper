---
name: container-images
category: infrastructure
description: Use when writing or fixing a Dockerfile, docker-compose file, or image build - multi-stage builds, layer caching, non-root runtime, healthchecks, image size, and build-time secrets
tech_stack: Docker
---

# Container Images

## Overview

**Core principle:** The image is a build artifact, reproducible from a clean checkout with one command. If it only builds on your machine, or only after a manual step, it is not done.

## Multi-stage is the default

```dockerfile
# syntax=docker/dockerfile:1
FROM node:22.23-bookworm-slim AS build
WORKDIR /app
COPY package*.json ./
RUN npm ci                      # dependencies cached on their own layer
COPY . .
RUN npm run build
RUN npm prune --omit=dev        # devDependencies must not reach the runtime layer

FROM node:22.23-bookworm-slim AS runtime
ENV NODE_ENV=production
WORKDIR /app
COPY --from=build /app/dist ./dist
COPY --from=build /app/node_modules ./node_modules
USER node                       # never root at runtime
EXPOSE 3000
HEALTHCHECK --interval=30s --timeout=3s --start-period=20s \
  CMD node -e "fetch('http://127.0.0.1:3000/health').then(r=>process.exit(r.ok?0:1)).catch(()=>process.exit(1))"
CMD ["node", "dist/main.js"]
```

The same shape per stack: `golang:x AS build` → `gcr.io/distroless/static` or `alpine`; `mcr.microsoft.com/dotnet/sdk:x AS build` → `mcr.microsoft.com/dotnet/aspnet:x`; a JVM build → a JRE runtime.

## Rules

| Concern | Do | Don't |
|---|---|---|
| Base image | A pinned, specific tag (`node:22.23-bookworm-slim`), digest-pinned where supply chain matters | `latest`, or a full OS image for a static binary |
| Layer order | Manifest/lockfile first, `RUN install`, then the source | `COPY . .` before installing dependencies — every edit re-installs |
| Ignore file | A real `.dockerignore`: `.git`, `node_modules`, `bin`, `obj`, `dist`, `.env*` | Shipping the build context wholesale (slow, and leaks files) |
| User | `USER` a non-root account, filesystem read-only where possible | Running as root because a port under 1024 was chosen |
| Config | Environment variables read at runtime | Baking environment config into the image (one image per environment) |
| Secrets | `RUN --mount=type=secret` for build-time credentials; runtime secrets from the platform | `ARG TOKEN=` — build args are visible in image history |
| Signals | An entrypoint that receives SIGTERM (exec form `CMD ["x"]`, or `tini` when you need a reaper) | A shell-form CMD that swallows the signal and forces a 10s kill |
| Size | Multi-stage, slim/distroless runtime, no build toolchain in the final layer | `apt-get install` of compilers in the runtime stage |

## Compose, for local and small deployments

Compose describes the whole local stack: the app, its database, its queue. Use `depends_on: condition: service_healthy` rather than sleeps, named volumes for data, and a `.env.example` that documents every variable with a fake value. `docker compose config` validates it before you trust it.

## Verify

```bash
docker build -t app:test .
docker run --rm -p 3000:3000 app:test &   # then hit the health endpoint
docker image inspect app:test --format '{{.Size}}'
hadolint Dockerfile
```

Also build it once with `--no-cache` before you call an image reproducible, and scan it (see devsecops-pipeline).

## Red flags

- `COPY --from=build node_modules` after a plain `npm ci` — the whole dev toolchain ships to production. Prune first, or install runtime dependencies in a stage of their own.
- The image builds but the container exits immediately — read `docker logs`, do not add a `sleep`.
- A `HEALTHCHECK` that only checks the process is alive (a hung process passes it).
- Data written inside the container with no volume — it disappears on the next deploy.
- The same image built twice produces different results because the build pulls unpinned dependencies at run time.
