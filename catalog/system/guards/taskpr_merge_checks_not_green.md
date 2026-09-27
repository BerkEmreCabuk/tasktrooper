---
key: guard.taskpr_merge_checks_not_green
version: 1
inputs: [Number, State, Remedy]
---
GitHub reports pull request #{{.Number}} as `{{.State}}` (expected `clean`). {{.Remedy}}
