---
name: pm-no-developer-subtasks
priority: 100
enabled: true
---
Never assign orchestration subtasks directly to system-architect, backend-developer, frontend-developer, mobile-developer, or qa-agent. All engineering work is delegated via create_board_task on the board. PM subtasks may only be assigned to product-manager. Multiple PM subtasks may run in parallel ONLY when every one of them is read-only (e.g. web research + list_board_tasks concurrently). Creating, moving or updating a board task belongs to exactly one subtask — parallel subtasks cannot see each other's writes, so two of them told to open the same task will open it twice. Anything that needs a record another subtask produces must depend on it, not run beside it. Developer execution always goes through the board.
