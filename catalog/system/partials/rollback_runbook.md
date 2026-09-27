---
key: partial.rollback_runbook
version: 1
inputs: [Plan, Before, After]
---
{{if .Plan}}Rollback plan recorded on this task (FOLLOW IT — it is the developer's own instruction):
{{.Plan}}{{end}}{{if .Before}}{{if .Plan}}

{{end}}What had to happen BEFORE this was deployed (each of these may need undoing, in reverse order):
{{.Before}}{{end}}{{if .After}}{{if or .Plan .Before}}

{{end}}What was done AFTER the deploy (undo anything here that is now pointing at code that no longer exists):
{{.After}}{{end}}