---
name: package-and-registry-management
category: infrastructure
description: Use when a change involves dependencies, lockfiles, private registries or publishing artifacts - npm/NuGet/Maven/Go/PyPI feeds and their CI authentication, container registries, versioning and automated dependency updates
---

# Packages, Registries and Dependencies

## Overview

**Core principle:** A build installs exactly what was reviewed. That means a committed lockfile, a frozen install in CI, and a registry the build authenticates to with a short-lived token.

## Installing in CI

| Ecosystem | Frozen install | Lockfile |
|---|---|---|
| npm | `npm ci` | `package-lock.json` |
| pnpm / yarn | `pnpm install --frozen-lockfile`; Yarn Berry (v2+) `yarn install --immutable`, Yarn 1.x `yarn install --frozen-lockfile` | `pnpm-lock.yaml` / `yarn.lock` |
| .NET | `dotnet restore --locked-mode` | `packages.lock.json` |
| Go | `go mod download` + `go mod verify` | `go.sum` |
| Python | `uv sync --frozen` / `pip install -r requirements.txt` (hashes) | lock or pinned requirements |
| Gradle | `gradle --no-daemon build` | dependency locking (`dependencyLocking {}`, `--write-locks`) |
| Maven | `mvn -B verify` | **no native lockfile** — pin every version explicitly and enforce it (maven-enforcer `banDynamicVersions`); `-B` is only batch mode and pins nothing |

A pipeline step that "updates" a lockfile during a build is a bug: the artifact then contains code nobody reviewed. Cache the package cache (`~/.npm`, `~/.nuget/packages`, the Go module cache) keyed on the lockfile hash — never the installed tree across different lockfiles.

## Private feeds

- Authenticate with a short-lived token from the CI's own identity (GitHub Packages via `GITHUB_TOKEN`, Azure Artifacts via a service connection, GCP/AWS via workload identity). A personal access token belonging to one engineer is a person-shaped outage.
- Config files are committed with the feed URL and no credential: `.npmrc` expands `${TOKEN}` from the environment, while NuGet does NOT — `nuget.config` needs `%TOKEN%` under `<packageSourceCredentials>` (or the credential added at build time with `dotnet nuget update source --username … --password …`). A literal `${TOKEN}` is sent as the password and fails with a 401 that reads like a permissions problem.
- Set the upstream/public feed order deliberately so an internal package name cannot be shadowed by a public one (dependency confusion). Scope internal packages (`@company/...`) and prefer a single virtual feed that proxies upstream.

## Publishing

Version with semver and tag the commit; the tag is what triggers the publish job. Never republish a version — consumers cache it, and some registries forbid it outright. Publish from CI only, from a protected branch or tag, with the token in that job alone. Attach a changelog and, for artifacts that matter, an SBOM (see devsecops-pipeline).

## Container registries

Tag with the immutable thing (the commit SHA or a semver release), and move `latest`/`stable` as a pointer only if the platform needs it. Set a retention/cleanup policy before the registry fills up, but never delete a tag a running deployment still references — check what is deployed first.

## Keeping dependencies current

Automate it (Renovate/Dependabot), group low-risk updates, and keep them mergeable: a pipeline that is red for unrelated reasons is why dependency PRs pile up for six months and a CVE fix takes a week. Majors get their own PR and a real read of the changelog.

## Red flags

- `npm install` (not `ci`) in a pipeline, or a lockfile that is gitignored.
- A token committed in `.npmrc`/`nuget.config`, or printed by a verbose install.
- Pinning to a floating tag for a dependency whose author can move it.
- A private package name that also exists on the public registry, with the public feed searched first.
