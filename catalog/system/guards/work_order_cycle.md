---
key: guard.work_order_cycle
version: 1
inputs: [Task, Blocker, Path]
---
work-order cycle refused: {{.Task}} already has to be finished before {{.Blocker}} ({{.Path}}), so it cannot also wait for it
