---
key: tool.list_runtime_errors
version: "1"
params:
    component: A component's own path (e.g. "apps/web", or "." for a single-purpose repo). Omit it (or pass ".") when the repository has only one component — it resolves automatically; with more than one it is required and the error names every path to choose from.
    environment: Which environment to read. Defaults to "production".
    repository_id: Repository UUID. Defaults to the run's own repository; pass this only to look at a different one.
    since: 'How far back to look, as a duration: "30m", "2h", "1d". Defaults to "24h".'
---
List an environment's runtime errors, grouped and deduplicated (native grouping where the provider has it, a message fingerprint otherwise) — a triage view, not a log dump. `new: true` means the group's first occurrence falls inside this window, the "started with this deploy" signal.
