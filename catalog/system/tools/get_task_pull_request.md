---
key: tool.get_task_pull_request
version: "1"
params:
    include_diff: Include the unified diff (default true). Set false when you only need the state, files or comments — the diff is by far the largest part of the result.
    task_id: Board task UUID or its board key (e.g. "T-1" for a task, "B-1" for a bug, "A-1" for an analysis). Optional in a chat that is already about one task — omit it there and the task in context is used.
---
Read the pull request opened for a board task: its state (open/draft/merged/mergeable), head and base branch, the changed-file list, the review comments and PR conversation, and — unless you turn it off — a size-capped diff. Use it before answering any question about "the PR" and before changing code a reviewer commented on. If the task has no PR yet, the result says so instead of failing.
