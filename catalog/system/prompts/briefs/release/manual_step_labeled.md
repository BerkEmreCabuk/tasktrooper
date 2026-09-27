---
key: briefs.release.manual_step_labeled
version: 1
inputs: [Label, Runbook]
---
{{.Label}}: {{partial "rollback_runbook" .Runbook}}