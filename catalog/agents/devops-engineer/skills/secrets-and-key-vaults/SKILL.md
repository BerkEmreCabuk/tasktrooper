---
name: secrets-and-key-vaults
category: security
description: Use when a change involves credentials, API keys, certificates or connection strings - choosing a secret store (Azure Key Vault, AWS/GCP Secrets Manager, Vault, Kubernetes, Coolify, CI secrets), wiring an app to read them, rotation, and what to do about a leaked secret
---

# Secrets and Key Vaults

## Overview

**Core principle:** Code holds the NAME of a secret; the platform holds its VALUE. Every secret has exactly one home, one way in (an identity, not another secret), and a rotation story.

You wire up references and document what a human must set. You do not invent, print, copy into a comment, or commit a secret value — ever, including into a private repository and including "temporarily".

## Choosing the store

| Where it runs | Store | How the app authenticates |
|---|---|---|
| GitHub Actions | Repository/environment secrets, or OIDC to a cloud role | Workload identity — no stored cloud key |
| Azure | Key Vault | Managed identity (`DefaultAzureCredential`) |
| AWS | Secrets Manager / Parameter Store | The workload's own IAM role: a task role on ECS, IRSA (service-account OIDC) on EKS, an instance profile on EC2 |
| GCP | Secret Manager | Workload identity |
| Kubernetes (any cloud) | External Secrets Operator syncing from the vault above, or Sealed Secrets/SOPS when the cluster is the source of truth | Service account |
| Coolify / small VPS | Coolify environment variables per app | The platform injects them |
| Local development | `.env` that is gitignored, seeded from a committed `.env.example` with fake values | — |

Prefer a federated identity (OIDC) over any long-lived key. The best secret is the one that does not exist.

## Wiring an app

1. Name the variable after what it is (`DATABASE__CONNECTIONSTRING`, `STRIPE__SECRETKEY`), and document it in `.env.example` and the README with its shape — never its value.
2. Read it at startup through the framework's configuration layer, and **fail fast**: a service that starts with a missing credential and errors on the first request is harder to diagnose than one that refuses to boot.
3. Never log it. Redact it in error handlers and in any diagnostics endpoint — connection strings end up in exception messages by default.
4. For certificates and keys, mount a file rather than passing multi-line values through environment variables.

## Rotation

Support two valid values at once (the old and the new) wherever the provider allows, so rotation is not an outage: add the new secret, deploy, verify, remove the old. Write the rotation steps next to the secret's documentation. A credential that cannot be rotated without downtime is an incident waiting for a bad week.

## A leaked secret

1. **Rotate it first.** Revoke at the provider and issue a new one. Everything else is secondary.
2. Remove it from the code and wire the reference properly.
3. Say clearly on the card that the old value is compromised, since git history keeps it. Purging history is a separate, coordinated action.
4. Check whether it was ever used from somewhere unexpected, if the provider offers an audit log.

## Red flags

- A secret passed as a Docker build `ARG` — it stays in the image history.
- A secret in a Kubernetes manifest in git, base64-encoded (that is encoding, not encryption).
- One shared key for every environment: a stage leak becomes a production breach.
- `echo $TOKEN` or `set -x` in a pipeline step that touches credentials.
- A key vault used as a config store: non-secret config belongs in a ConfigMap/appsettings, where reading it is not an audited event.
