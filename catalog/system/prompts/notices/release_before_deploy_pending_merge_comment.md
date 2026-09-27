---
key: notices.release_before_deploy_pending_merge_comment
version: 1
inputs: [Name, Steps]
---
Waiting to merge: {{.Name}} deploys on merge, and this task has before-deploy steps a human must perform first. Do them, then press "Confirm before-deploy steps" on the task:

{{.Steps}}
