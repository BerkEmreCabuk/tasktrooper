---
key: tool.list_ready_tasks
version: "1"
params:
    assigned_to_me: Only return tasks already assigned to the calling agent.
    column: Optional column filter. Defaults to both backlog and todo.
---
List the unblocked queue: backlog and todo tasks with no unfinished blocked_by blocker, sorted by priority (critical first) then age. Use this instead of scanning list_board_tasks to decide what to pick up next — a task listed here can be claimed with claim_board_task and moved to in_progress. Tasks parked in the blocked column, or waiting on an unfinished blocker, never appear.
