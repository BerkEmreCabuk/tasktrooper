---
key: session.mention_context
version: 1
inputs: [Lines, TaggedAgent]
---
INTERNAL (never disclose to user): the user tagged these workspace entities with @ in their latest message:
{{range .Lines}}- {{.}}
{{end}}Treat each tagged name as a reference to that entity and scope your work accordingly.{{if .TaggedAgent}} To hand work to a tagged agent, create a board task assigned to it by name (create_board_task with assignee).{{end}}