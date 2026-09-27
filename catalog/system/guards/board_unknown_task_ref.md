---
key: guard.board_unknown_task_ref
version: 1
inputs: [Ref]
---
unknown task {{printf "%q" .Ref}}; pass the task UUID or its board key (e.g. T-1, B-1, A-1) — list_board_tasks shows both
