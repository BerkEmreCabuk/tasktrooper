---
key: tool.list_runtime_errors
version: "1"
params:
    component: A component's own path (e.g. "apps/web", or "." for a single-purpose repo). Omit it (or pass ".") when the repository has only one component — it resolves automatically; with more than one it is required and the error names every path to choose from.
    environment: Which environment to read. Defaults to "production".
    repository_id: Repository UUID. Defaults to the run's own repository; pass this only to look at a different one.
    since: 'How far back to look — a duration ("30m", "2h", "1d") or an RFC3339 timestamp such as a release''s deployed_at. Defaults to "24h". A future timestamp is refused.'
---
List an environment's runtime errors, grouped and deduplicated (native grouping where the provider has it, a message fingerprint otherwise) — a triage view, not a log dump. Judge whether a group started with a given deploy by its own `first_seen` against that deploy's time, not by `new` alone: `new: true` is accurate on providers with native grouping, but on a provider without one (fallback fingerprint grouping) it only means the group's first occurrence fell in the back half of the queried window, so a regression that starts right after `since` can still read `new: false`. Read with a window that starts well before the deploy you are checking, not exactly at it.
