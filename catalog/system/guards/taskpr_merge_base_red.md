---
key: guard.taskpr_merge_base_red
version: 1
inputs: [BaseRef, BaseSHA, FailingChecks]
---
required checks fail on {{.BaseRef}}@{{.BaseSHA}} too: {{.FailingChecks}} — pre-existing on the base branch, not introduced by this pull request
