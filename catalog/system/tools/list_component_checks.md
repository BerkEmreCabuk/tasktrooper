---
key: tool.list_component_checks
version: "1"
params:
    component: A component's own path (e.g. "apps/web", or "." for a single-purpose repo) to list just that component's checks.
    repository_id: Repository UUID. Defaults to the run's own repository; pass this only to look at a different one.
---
List the CI checks mapped onto each component: what job runs it, what it verifies, whether CI gates on it, and the local command that reproduces it. Use this to find the exact command CI runs before you claim something builds or passes.
