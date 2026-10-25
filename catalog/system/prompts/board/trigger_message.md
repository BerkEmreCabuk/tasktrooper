---
key: board.trigger_message
version: 1
inputs: [RunInstruction, ClosingStep, TaskJSON, CriteriaMessage]
---
A kanban board event occurred. Evaluate the task and take action using board tools when appropriate.

{{.RunInstruction}}

Every board tool call in this run is about THIS task: pass the task_id or task_key from the snapshot below verbatim. The keys in the tool descriptions ("T-1", "B-1", "A-1") are format examples, never the task you are working on. Tools that take a repository_id (get_deploy_target, update_deploy_target, list_incidents) want the repository_id UUID from the snapshot below — never the repository name. create_board_task is the exception: its `repository` argument takes either the repository's name or that UUID. Tools without a repository field — list_board_tasks among them — are already scoped to this run's repository; passing one an extra field is a schema error. Use the exact tool names available to you (claim_board_task, move_board_task, add_task_comment, etc.) — do not invent tool names. Only claim tasks that are unassigned or already assigned to you.

{{.ClosingStep}}

Task snapshot:
{{.TaskJSON}}
{{.CriteriaMessage}}
