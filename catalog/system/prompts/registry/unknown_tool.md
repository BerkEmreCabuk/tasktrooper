---
key: registry.unknown_tool
version: 1
inputs: [Called, Available, Suggestion]
---
{{if not .Available}}unknown tool: {{.Called}}. No tools are available for this run.{{else}}unknown tool: {{.Called}}. It does not exist — do not call it again.{{if .Suggestion}} Did you mean {{.Suggestion}}?{{end}} Available tools: {{join ", " .Available}}.{{end}}
