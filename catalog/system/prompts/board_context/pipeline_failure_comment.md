---
key: board_context.pipeline_failure_comment
version: 1
inputs: [Stage, Report]
---
Pipeline failed — {{.Stage}}:

```
{{.Report}}
```
