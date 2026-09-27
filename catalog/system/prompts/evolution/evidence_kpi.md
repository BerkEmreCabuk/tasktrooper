---
key: evolution.evidence_kpi
version: 1
inputs: [Lines, Composite]
---
## KPI attainment (your objectives)
{{range .Lines}}- {{.Name}} ({{.MetricKey}}, {{.Period}}): full {{printf "%.4g" .TargetFull}} / half {{printf "%.4g" .TargetHalf}}{{if .HasResult}} | measured {{printf "%.4g" .MeasuredValue}} → attainment {{printf "%.0f" .AttainmentPct}}%{{end}}
{{end}}Composite KPI score: {{.Composite}}/100


