---
key: guard.taskpr_merge_task_not_done
version: 1
inputs: [Label, Column]
---
{{.Label}} is in `{{.Column}}`. A pull request is merged when the board has signed the task off, not while it is still being reviewed or tested
