---
key: clarification.format_message
version: 1
inputs: [Context, Questions]
---
I need a few details before I can continue:

{{if .Context}}{{.Context}}

{{end}}{{range .Questions}}{{.Number}}. {{.Prompt}}
{{range .Options}}   - {{.}}
{{end}}
{{end}}Please reply with your choices or additional details.
