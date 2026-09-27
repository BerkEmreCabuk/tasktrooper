---
key: tool.list_task_comments
version: "1"
params:
    limit: How many of the most recent comments to return (default 20).
    task_id: Board task UUID or its board key (e.g. "T-1" for a task, "B-1" for a bug, "A-1" for an analysis). Optional in a chat that is already about one task — omit it there and the task in context is used.
---
READ the comments on a board task — the reviewer's revision feedback, the human's answers and every earlier agent's notes, oldest first. Use it before starting work on a task that came back from review, and whenever you are about to say "let me check the comments". This tool only reads; add_task_comment is what writes one. Reviewer notes left on the PULL REQUEST are not here — get_task_pull_request returns those.
