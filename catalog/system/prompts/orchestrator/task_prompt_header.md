---
key: orchestrator.task_prompt_header
version: "1"
inputs: [HasPurpose, Purpose, Goal, Title, Description, SkillFocus]
---
{{if .HasPurpose}}Purpose: {{.Purpose}}
Goal: {{.Goal}}

{{end}}Task: {{.Title}}

{{.Description}}

{{if .SkillFocus}}{{.SkillFocus}}

{{end}}