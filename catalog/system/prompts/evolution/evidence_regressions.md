---
key: evolution.evidence_regressions
version: 1
inputs: [Lines]
---
## ⚠ Regressed changes (your earlier changes that hurt performance — consider reverting)
{{range .Lines}}- event_id {{.EventID}} | {{.ChangeType}} {{.TargetName}} ({{.Date}}) | performance DROPPED after this change. Before-state is stored; add it to reverts[] to undo.
{{end}}

