---
key: orchestrator.clarification_blocked_reason
version: "1"
inputs: [Detail]
---
{{if .Detail}}waiting for an answer: {{.Detail}}{{else}}waiting for an answer from the stakeholder{{end}}