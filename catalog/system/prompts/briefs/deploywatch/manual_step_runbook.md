---
key: briefs.deploywatch.manual_step_runbook
version: 1
inputs: [Runbook]
---
The task recorded its own rollback instructions. Perform each of them yourself and report what you did, or say clearly which ones you could not:
{{partial "rollback_runbook" .Runbook}}