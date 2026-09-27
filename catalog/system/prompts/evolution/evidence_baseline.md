---
key: evolution.evidence_baseline
version: 1
inputs: [Date, SnapshotJSON, Summary]
---
## Baseline (previous reflection, {{.Date}})
{{.SnapshotJSON}}
{{if .Summary -}}
Previous self-assessment: {{.Summary}}
{{end -}}
Compare current performance against this baseline: did your last changes help or hurt?


