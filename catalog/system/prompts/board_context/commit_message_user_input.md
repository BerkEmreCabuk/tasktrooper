---
key: board_context.commit_message_user_input
version: 1
inputs: [Title, Summary]
---
Task title: {{.Title}}{{if .Summary}}

What the agent reports it did:
{{.Summary}}{{end}}
