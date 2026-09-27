---
key: orchestrator.verifier_user
version: "1"
inputs: [UserMessage, Purpose, Goal, TaskResultsBlock]
---
User request: {{.UserMessage}}

Purpose: {{.Purpose}}

Goal: {{.Goal}}

Task results:
{{.TaskResultsBlock}}