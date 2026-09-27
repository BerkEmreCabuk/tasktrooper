---
key: tool.finish_release
version: "1"
params:
    note: 'What you checked before confirming: the log window you read, the error groups (or their absence), the smoke results. Goes on the release as its verdict.'
    task_id: Board task UUID or its board key (e.g. "T-1" for a task, "B-1" for a bug, "A-1" for an analysis). Optional in a chat that is already about one task — omit it there and the task in context is used.
---
Confirm this release as shipped: moves every task it carries to `released`. This is the ONLY way a task may reach `released` — never move the card there yourself. Call it only while the release is `awaiting_verdict`, and only after you have actually read the evidence in THIS run: query_runtime_logs and list_runtime_errors since deployed_at, and the release's own health/smoke checks (get_release). A `failed` release can only be finished by a human overriding it ("ship it anyway") — an agent call is refused. `note` is required and must say what you actually checked (the log window, the error groups, the smoke results), not just "looks fine".
