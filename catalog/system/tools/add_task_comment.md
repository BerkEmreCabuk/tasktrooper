---
key: tool.add_task_comment
version: "1"
params:
    content: Comment text
    task_id: Board task UUID or its board key (e.g. "T-1" for a task, "B-1" for a bug, "A-1" for an analysis).
---
Add a comment to a board task as the current agent. Comment when something needs a person or the next agent to ACT: a rejection and why, a failure with its expected-vs-actual, a blocker, a question you cannot answer yourself, work you did not do. Do NOT comment to report that things went well — a passed review, a green build, a successful merge or deploy, a criterion you approved, "moving to X". The column, the criteria, the pull request, the pipeline result and the task history already carry all of that, and a thread of confirmations buries the one comment that mattered. Never paste the pull request link or number either: it is a field on the task and the board renders it.
