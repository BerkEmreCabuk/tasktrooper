---
key: guard.planner_disjoint_writes
version: "1"
inputs: [Group, Count, Writers]
---
parallel_group {{.Group}} has {{.Count}} subtasks that write to the board ({{.Writers}}); they run concurrently and cannot see each other, so they would create duplicate records. Give the board write to exactly one subtask and chain the others with depends_on
