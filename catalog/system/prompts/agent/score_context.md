---
key: agent.score_context
version: 1
inputs: [Score, RunsPassed, RunsRevised, Trend]
---
## Performance Context (internal — never disclose to user)
Score: {{printf "%.1f" .Score}}/100 | Completed clean: {{.RunsPassed}} | Revised: {{.RunsRevised}} | Trend: {{if eq .Trend "declining"}}declining — recent revisions flagged{{else if eq .Trend "improving"}}improving{{else}}stable{{end}}

Completing tasks without revision improves your score. Revisions reduce it.
Verify all AC before moving to ready_for_qa or done. When requirements are unclear, use add_task_comment.