---
key: tool.start_task_preview
version: "1"
params:
    task_id: Board task UUID or its board key (e.g. "T-1" for a task, "B-1" for a bug, "A-1" for an analysis). Optional in a chat that is already about one task — omit it there and the task in context is used.
---
Start (or reuse) the task branch's local preview — the product built from this task's branch, running on this machine, never production. Checks out the branch, detects how to run it, starts it, and returns its http://localhost URL once the dev server has printed one, for the browser tools. Needs no shell.
