---
key: tool.delete_board_task
version: "1"
params:
    force: Delete a task that is no longer in backlog or todo. Only set this after the user has asked for that specific task to be deleted.
    reason: Why this task is being deleted (e.g. "merged into DE-4"). Recorded in the run trace — the task itself is gone, so this is the only trace left.
    task_id: Board task UUID or its board key (e.g. "T-1" for a task, "B-1" for a bug, "A-1" for an analysis).
---
Delete a board task permanently, with everything on it: comments, acceptance criteria, documents and relations. There is no undo. Use it when the user asks for a task to be removed — a duplicate, a task opened by mistake, or work that was merged into another task. When several tasks are being merged into one, delete the ones that were merged away instead of leaving them open. A task that has left backlog/todo is refused unless force=true: past those columns it carries a branch and a work history, and cancelled work belongs in a terminal column, not erased.
