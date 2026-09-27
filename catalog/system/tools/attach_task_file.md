---
key: tool.attach_task_file
version: "1"
params:
    attachment_id: UUID of the already-uploaded attachment.
    task_id: Board task UUID or its board key (e.g. "T-1" for a task, "B-1" for a bug, "A-1" for an analysis).
---
Attach an already-uploaded file (image/document) to a board task. Use when the conversation/task context references an uploaded file that belongs on the task.
