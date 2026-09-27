---
key: orchestrator.replanner_user
version: "1"
inputs: [UserMessage, Purpose, Goal, IssuesJoined, ExistingPlanJSON, TaskResultsBlock]
---
User request: {{.UserMessage}}

Purpose: {{.Purpose}}

Goal: {{.Goal}}

Verification issues:
- {{.IssuesJoined}}

Existing plan:
{{.ExistingPlanJSON}}

Task results:
{{.TaskResultsBlock}}