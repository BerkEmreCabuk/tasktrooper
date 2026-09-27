---
key: briefs.prodops.remediation_task
version: 1
inputs: [Env, Severity, Source, Occurrences, Title, Detail, RemedyKind, Confidence, RemedyText, IncidentID, AutoFix]
---
Production incident on **{{.Env}}** (severity: {{.Severity}}, source: {{.Source}}, occurrences: {{.Occurrences}}).

**Symptom:** {{.Title}}
{{if .Detail}}
```
{{.Detail}}
```
{{end}}
**First-pass hypothesis ({{.RemedyKind}}, confidence {{.Confidence}}):**
{{.RemedyText}}

Incident id: `{{.IncidentID}}` — call `get_incident` for the full payload and timeline.
{{if .AutoFix}}
**Policy: auto_fix.** Diagnose, then implement the fix and take it through the normal pipeline. Record what you concluded with `propose_incident_remedy` before you start changing code.
{{else}}
**Policy: suggest.** Do NOT change production code. Diagnose only, then call `propose_incident_remedy` with the concrete fix (commands, files, config) and move the task to human_uat for the decision.
{{end}}
