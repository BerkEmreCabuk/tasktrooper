---
name: deploy-templates
category: operations
description: Author a repository's deploy from the built-in provider recipes (GCP, AWS, Vercel, Fly) instead of hand-rolling a workflow, and make it verifiable and reversible.
---

# Deploy templates

Deploying is a defined step like build and test, not improvisation. Every environment
(`stage`, `preprod`, `prod`) of a repository has a **deploy target**: a provider, its
variables, a health URL and a rollback policy. Each provider has a **template**: the
workflow to write, the secrets it needs, the smoke check and the rollback.

## Workflow

1. `list_deploy_templates` (optionally filtered by repo kind) to see what is available.
2. `get_deploy_target` with the repository id and env to read what this repo actually
   ships to, plus the recipe already rendered with its variables.
3. `load_deploy_template` for the full recipe when you need the raw version.
4. Write the workflow into `.github/workflows/` exactly as the recipe describes. Keep the
   `workflow_dispatch` trigger: the board dispatches deploys by workflow file.
5. Map the workflow under Repository Settings → Pipeline for the matching category
   (`stage_deploy` / `preprod_deploy` / `prod_deploy`), otherwise nothing will ever run it.

## Non-negotiables

- **A deploy must verify itself.** A green deploy step that never checked the service is a
  lie. Every deploy ends with a smoke check against the environment's health URL.
- **A deploy must be reversible.** Include the rollback step from the recipe (`if: failure()`).
  If a provider cannot roll back automatically, document the exact manual command.
- **Wait for the rollout.** `kubectl rollout status`, `aws ecs wait services-stable` and
  their equivalents are what make the result honest; dropping them for speed hides
  crash-looping releases.
- **No secrets in the workflow.** Use OIDC/Workload Identity Federation and repository
  secrets. Never commit a key file.
- **Concurrency guard.** One deploy per environment at a time (`concurrency.group`),
  otherwise two releases race and the environment ends up in an undefined state.
- If a required variable is missing (the rendered recipe shows `{{key — SET THIS}}`), ask
  for it. Do not guess a project id, region or cluster name.
