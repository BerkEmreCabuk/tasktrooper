---
key: guard.release_before_deploy_pending_merge
version: 1
inputs: [Name, Steps]
---
{{.Name}} deploys on merge, and this task's before-deploy steps are not confirmed — a human must perform them and press "Confirm before-deploy steps" on the task before it can merge. Nothing was merged. Do not retry — you will be woken when a human confirms:

{{.Steps}}
