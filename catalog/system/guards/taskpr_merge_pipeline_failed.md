---
key: guard.taskpr_merge_pipeline_failed
version: 1
inputs: [Trigger, Detail]
---
the last {{.Trigger}} pipeline for this task FAILED.{{.Detail}} Send the task back to need_revision so the developer fixes it; a red build is not merged and then fixed on the default branch
