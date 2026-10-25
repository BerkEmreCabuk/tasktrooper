---
key: tool.query_runtime_logs
version: "1"
params:
    component: A component's own path (e.g. "apps/web", or "." for a single-purpose repo). Omit it (or pass ".") when the repository has only one component — it resolves automatically; with more than one it is required and the error names every path to choose from.
    environment: Which environment to read. Defaults to "production".
    limit: Maximum entries to return. Defaults to 100, capped at 500.
    min_severity: Only entries at or above this severity.
    repository_id: Repository UUID. Defaults to the run's own repository; pass this only to look at a different one.
    since: 'How far back to read — a duration ("30m", "2h", "1d") or an RFC3339 timestamp such as a release''s deployed_at. Defaults to "1h". A future timestamp is refused.'
    text: Free-text filter over the log message.
---
Read an environment's live application logs — what the running product itself printed, not a CI job's output (that is get_deploy_logs). Use this to see what a request actually did in production or on stage instead of guessing from the code.
