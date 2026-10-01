---
key: board_context.analysis_questions
version: 1
inputs: [Label, TaskRef, Questions, OmittedCount]
---

## Open questions{{if .Label}} — {{.Label}}{{end}}
{{range .Questions}}
### {{.Key}} ({{.Kind}}{{if .Blocking}}, blocking{{end}})
{{.Prompt}}
{{if .Answered}}Answer (human): {{.Answer}}{{else if .Blocking}}Unanswered (blocking){{else}}Unanswered — recommended answer stands: {{.RecommendedAnswer}}{{end}}
{{end}}{{if .OmittedCount}}
…{{.OmittedCount}} more question(s) are not shown here. Call list_open_questions with task_id {{.TaskRef}} to read all of them.
{{end}}
