---
key: board_context.pipeline_failure_blocked_ci_comment
version: 1
inputs: [Stage, Report]
---
Deploy could not run — {{.Stage}}:

```
{{.Report}}
```

GitHub Actions is unavailable for this repository (billing, spending limit or Actions disabled), so this is not a code problem and the task is NOT being sent back to need_revision. Deploy it the way this repository documents doing it locally, or move the task to `blocked` if it has no local deploy path.
