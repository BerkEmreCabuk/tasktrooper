---
key: briefs.prodops.release_attribution_comment
version: 1
inputs: [Note, Title, Env, Severity, Detail]
---
Production incident inside this release's health window.

{{.Note}}

Incident: {{.Title}} ({{.Env}}, {{.Severity}})
{{.Detail}}
