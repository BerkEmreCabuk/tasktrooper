---
key: briefs.repository.order_note
version: 1
inputs: [DeployAfter, WorkAfter]
---
**Release order (generated from this task's relations — do not edit by hand):**
{{if .DeployAfter}}- Ships after: {{join ", " .DeployAfter}}. Each one must be live in production before this task is released; the release is refused otherwise.
{{end}}{{if .WorkAfter}}- Built after: {{join ", " .WorkAfter}}. Work on this task does not start until those are done.
{{end}}