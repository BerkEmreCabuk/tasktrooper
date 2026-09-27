---
key: orchestrator.synthesize_user
version: "1"
inputs: [UserMessage, Summary, FullResults]
---
User request: {{.UserMessage}}

Plan summary: {{.Summary}}

Task results:
{{.FullResults}}
