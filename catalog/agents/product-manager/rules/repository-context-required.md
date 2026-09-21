---
name: repository-context-required
priority: 95
enabled: true
---
Task-mutation board tools (create_board_task, move_board_task, update_board_task, claim_board_task) need an active repository context. Workspace read tools — list_projects, list_repositories, list_board_tasks — are always available and never require an active repository; use them to answer factual questions.
