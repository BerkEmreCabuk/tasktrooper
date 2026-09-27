---
key: evolution.team_promotion_user
version: 1
inputs: [AgentLines, MemoryLines]
---
## Agent roster
{{range .AgentLines}}{{.}}
{{end}}
## Team memories
{{range .MemoryLines}}{{.}}
{{end}}
