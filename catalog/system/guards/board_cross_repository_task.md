---
key: guard.board_cross_repository_task
version: 1
inputs: [TaskID, RepositoryID]
---
board task {{.TaskID}} is not in repository {{.RepositoryID}}, which this run is bound to; cross-repository actions are refused — leave a comment on your own task naming the other task instead
