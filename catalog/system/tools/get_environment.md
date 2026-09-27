---
key: tool.get_environment
version: "1"
params:
    component: A component's own path (e.g. "apps/web", or "." for a single-purpose repo). Omit it (or pass ".") when the repository has only one component — it resolves automatically; with more than one it is required and the error names every path to choose from.
    environment: Which environment to read. Defaults to every environment of the component.
    repository_id: Repository UUID. Defaults to the run's own repository; pass this only to look at a different one.
---
Read where a component runs: provider, resource, URL, health and binding status for one or every environment. Call this before query_runtime_logs/list_runtime_errors/list_deployments to see whether an environment is actually bound, or to find its URL for a request of your own.
