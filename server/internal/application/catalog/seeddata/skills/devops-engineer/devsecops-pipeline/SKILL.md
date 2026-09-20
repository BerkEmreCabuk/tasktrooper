---
name: devsecops-pipeline
category: security
description: Use when adding or fixing security controls in a pipeline or repository - secret scanning, dependency and container scanning, SAST, SBOM, supply-chain pinning, branch protection and least-privilege CI permissions
---

# DevSecOps in the Pipeline

## Overview

**Core principle:** Security checks are pipeline jobs with the same standing as tests — they run on every PR, they fail the build on the severity the team agreed, and their findings are fixed rather than muted. A scanner nobody can pass gets disabled within a week; pick thresholds the repository can actually hold.

## The layers, cheapest first

| Layer | What it catches | Typical tool |
|---|---|---|
| Secret scanning | Credentials about to be committed, or already in history | gitleaks / trufflehog, plus the platform's own push protection |
| Dependency scanning | Known CVEs in libraries | `npm audit`, `dotnet list package --vulnerable`, `govulncheck`, OSV-Scanner, Dependabot/Renovate alerts |
| SAST | Injection, unsafe deserialization, crypto misuse | CodeQL, Semgrep, `gosec`, language linters with security rules |
| Container scanning | Vulnerable base image and OS packages | Trivy, Grype |
| IaC scanning | Public buckets, permissive security groups, privileged pods | `trivy config`, Checkov or tfsec for cloud/Terraform; kube-score only lints Kubernetes manifests and produces no cloud findings |
| SBOM | What is actually in the artifact, for when a CVE lands tomorrow | Syft (`syft <image> -o spdx-json`) or `docker scout sbom` — the old `docker sbom` plugin is gone — attached to the release |

Run the fast ones (secret scan, dependency scan, lint) on every PR; the slower ones (SAST full scan, image scan) on main and nightly, so PR feedback stays minutes long.

## Supply chain

- Pin third-party CI actions to a commit SHA, not a tag. A tag can be moved onto new code; a SHA cannot.
- Pin base images by digest for anything that ships to production.
- Lockfiles are committed and installed from (`npm ci`, `go mod download` followed by `go mod verify`, `dotnet restore --locked-mode`), so CI installs exactly what was reviewed. `go mod verify` alone installs nothing — it checks what is already in the module cache.
- Automate dependency updates (Dependabot/Renovate) with grouped PRs, and keep the pipeline green enough that the team merges them.

## Least privilege in CI

- `permissions: contents: read` as the workflow default. A job needing more restates every scope it needs — a job block REPLACES the workflow's map rather than adding to it, so `id-token: write` alone drops `contents: read` and checkout fails. A job that only runs tests has no business with `packages: write`.
- Cloud access via OIDC federation to a scoped role, so there is no long-lived key to leak. When a static key is unavoidable, it is scoped to one environment and rotated on a schedule.
- Fork PRs never receive secrets: keep deploy/publish jobs off `pull_request` triggers, and never use `pull_request_target` with a checkout of the fork's head.
- Protect `main`: required checks, required review, no force-push, no direct push.

## When a scanner fires

Fix, upgrade, or — if neither is possible now — record a time-boxed, written exception with the reason and the compensating control. Never a blanket ignore file. A finding in a dev-only dependency is still triaged, not deleted.

## Committed secrets

A credential in git history is compromised the moment it lands, and deleting the line does not remove it. Rotate first, then purge history if the repository is private and small enough to rewrite. Say both on the card; rotation is usually a human's action.

## Red flags

- `continue-on-error: true` on a security job (it reports forever and blocks nothing).
- A scanner configured to fail only on CRITICAL, in a repository whose last three incidents were HIGH.
- `.env` in the repository with anything that is not obviously fake.
- Security jobs that run only on a schedule, so a PR can merge a known-vulnerable dependency and nobody sees it for a day.
