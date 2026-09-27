---
key: tool.list_deployments
version: "1"
params:
    component: A component's own path (e.g. "apps/web", or "." for a single-purpose repo). Omit it (or pass ".") when the repository has only one component — it resolves automatically; with more than one it is required and the error names every path to choose from.
    environment: Which environment to read. Defaults to "production".
    limit: Maximum deployments to return. Defaults to 10.
    repository_id: Repository UUID. Defaults to the run's own repository; pass this only to look at a different one.
---
List an environment's recent deployments (status, commit, branch, URL, timestamps) as the provider reports them, newest first.
