---
name: github-actions-pipelines
category: ci-cd
description: Use when creating or changing a GitHub Actions workflow - job/matrix structure, caching, reusable workflows, environments and approvals, OIDC instead of stored cloud keys, and why a pipeline is slow or flaky
tech_stack: GitHub Actions
---

# GitHub Actions Pipelines

## Overview

**Core principle:** One pipeline per repository, extended — not a second workflow beside the one that exists. Read `.github/workflows/` before adding anything, and follow its naming, triggers and job layout.

## Before editing

0. Check the current major version of every action you add (the repo's other workflows, or the action's own README) — majors move, and a version from memory is how a workflow fails on its first run.
1. `get_pipeline_status` — is the pipeline currently green? A change on top of a red pipeline makes the failure yours to explain.
2. Read every existing workflow: which triggers are taken, which jobs already build/test, what is already cached.
3. `actionlint` on what you write (`brew install actionlint` locally, or the action in CI). A typo in a `needs:` or an `if:` is silent otherwise.

## Structure

```yaml
name: ci
on:
  push: { branches: [main] }
  pull_request:
permissions:
  contents: read          # workflow default; a job block REPLACES this, it does not add to it
concurrency:
  group: ci-${{ github.workflow }}-${{ github.ref }}
  cancel-in-progress: true   # never on a deploy workflow — a half-run deploy must finish
jobs:
  build:
    runs-on: ubuntu-latest
    timeout-minutes: 15    # every job: the default is 6 hours of a stuck runner
    steps:
      - uses: actions/checkout@<sha>        # current major, pinned to a SHA
      - uses: actions/setup-node@<sha>
        with: { node-version-file: .nvmrc, cache: npm }
      - run: npm ci
      - run: npm test
```

Rules that hold in every workflow:

| Concern | Do | Don't |
|---|---|---|
| Permissions | `permissions: contents: read` at the top; a job needing more restates EVERY scope it needs (`contents: read` **and** `id-token: write`) — a job block replaces the workflow's map rather than merging with it, and unlisted scopes become `none` | A job block with only `id-token: write`, which silently drops checkout's `contents: read` |
| Third-party actions | Pin to a full commit SHA with the version in a comment | `@main`, or a floating major tag on an unaudited action |
| First-party token | Check the repository's Actions settings: `GITHUB_TOKEN` is read-only by default for repositories created since Feb 2023, but an older repo can still default to write-all | Assuming the default is safe without looking |
| Versions | Read the repo's own pin (`.nvmrc`, `go.mod`, `global.json`, `.tool-versions`) | Hardcoding a version the repo does not declare |
| Caching | The setup action's built-in cache, keyed on the lockfile | Caching `node_modules`/`obj` across different lockfiles |
| Repetition | A reusable workflow (`workflow_call`) or a composite action | Copy-pasting a 40-line job into five workflows |
| Matrix | `strategy.matrix`, with `fail-fast: false` as a SIBLING of `matrix` under `strategy` (nested inside `matrix` it becomes another dimension and doubles the jobs) | A matrix over things that never differ |
| Secrets | `secrets:` passed explicitly into the reusable workflow; OIDC to a cloud role where the provider supports it | Long-lived cloud keys in repo secrets |
| Deploys | A job gated on `environment:` with required reviewers for prod | A deploy step inside the same job as the tests |
| PRs from forks | `pull_request` (no secrets) for tests | `pull_request_target` with a checkout of the PR head — that hands the fork your token |

## Fast and honest

- Split so a failure names itself: `lint`, `test`, `build` as separate jobs rather than one script whose log has to be excavated.
- `needs:` expresses real dependencies only — jobs with no dependency run in parallel for free.
- Upload the artifact a failure needs (test report, screenshots, logs) with `if: always()`, or the evidence dies with the runner.
- A flaky job is a bug to fix, never a `continue-on-error` or a retry loop. Retrying is acceptable only around a genuinely external dependency, and the reason goes in the step name.

## Verify

`actionlint` is the syntax check — `act` is not a linter, it executes the workflow in Docker and cannot run macOS or Windows runners at all. Then push the branch and read the run with `get_pipeline_status`. A workflow that has never run is not a working workflow — say so on the card if the run has not happened yet.

## Red flags

- `if: github.ref == 'refs/heads/main'` guarding a deploy inside a PR-triggered workflow (it never runs, and nobody notices).
- A job-level `permissions:` block that lists one scope and silently drops the others the job needs.
- A secret echoed into a step's output or written into a file that a later step uploads as an artifact.
- `actions/checkout` with `persist-credentials: true` (the default) in a job that runs untrusted code.
- A workflow whose only test of correctness was that the YAML parsed.
