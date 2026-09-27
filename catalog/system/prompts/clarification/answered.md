---
key: clarification.answered
version: 1
inputs: [ShownCount, OmittedCount, Answers]
---
## Clarifications already answered on this task
The human has already answered the questions below. Treat these answers as decided requirements, act on them, and never ask them again — only ask about something genuinely still unknown.
{{if .OmittedCount}}The {{.ShownCount}} most recent are shown; {{.OmittedCount}} older answer(s) are on this task's comments — read them with list_comments before treating anything as unanswered.
{{end}}{{range .Answers}}
{{.}}
{{end}}
