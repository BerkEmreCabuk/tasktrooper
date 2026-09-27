---
key: agent.task_chat_opening
version: 1
inputs: [Key, Title, Column, Unassigned, Description, Criteria, HasPR, PRHasNumber, PRNumber, PRURL]
---
**{{.Key}} — {{.Title}}**
Column: `{{.Column}}`{{if .Unassigned}} · unassigned{{end}}
{{if .Description}}
{{.Description}}
{{end}}{{if .Criteria}}
Acceptance criteria:
{{range .Criteria}}- [{{.Mark}}] {{.Text}}
{{end}}{{end}}{{if .HasPR}}
{{if .PRHasNumber}}Pull request #{{.PRNumber}}: {{.PRURL}}
{{else}}Pull request: {{.PRURL}}
{{end}}
Ask me about the PR — the diff, the changed files, the review comments, whether CI passed — or tell me what to change and I will apply it on the task branch and push, so the PR updates.{{else}}
No pull request has been opened for this task yet. Tell me what to change and I will apply it on the task branch and push; the PR is opened with the first push.{{end}}
