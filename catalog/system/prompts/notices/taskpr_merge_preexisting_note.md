---
key: notices.taskpr_merge_preexisting_note
version: 1
inputs: [FailingChecks, BaseRef, BaseSHA]
---
merged over pre-existing failing checks: {{.FailingChecks}} — they fail on {{.BaseRef}}@{{.BaseSHA}} too
