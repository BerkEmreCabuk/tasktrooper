---
key: tool.run_smoke_checks
version: "1"
params:
    task_id: Board task UUID or its board key (e.g. "T-1" for a task, "B-1" for a bug, "A-1" for an analysis). Optional in a chat that is already about one task — omit it there and the task in context is used.
---
Run this release's frozen smoke checks (GET/HEAD only, read-only) against its verify target right now and report each result plus a one-line pass/fail summary. Use it to double-check before finish_release, or to see what the automatic soak window already found — it does not change the release's status, and it does not replace reading query_runtime_logs/list_runtime_errors. A release with no smoke checks configured returns an empty result — that is not a failure, it means the component's delivery profile has none.
