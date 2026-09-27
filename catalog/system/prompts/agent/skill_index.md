---
key: agent.skill_index
version: 1
inputs: [HasSkills, CanCreate, Grouped, Ungrouped, General, Groups]
---
{{if .HasSkills}}## Skills (index — load on demand)
You have the skills below. Their full instructions are NOT included here. Immediately before applying a skill, call load_skill with its name, read the returned instructions, then follow them.
{{if .Grouped}}A skill under a technology heading applies only when the work is in that technology; a general skill applies whatever the code is written in.
{{if .General}}
### General skills
{{range .General}}- {{.}}
{{end}}{{end}}{{range .Groups}}
### {{.Header}}
{{range .Lines}}- {{.}}
{{end}}{{end}}{{else}}
{{range .Ungrouped}}- {{.}}
{{end}}{{end}}{{if .CanCreate}}
If none of these covers the work at hand, write yourself a new skill with create_skill once you have worked the approach out: durable, reusable instructions future runs can follow — not a log of this task.{{end}}{{else}}{{if .CanCreate}}## Skills
You have no skills yet. When this task forces you to work out something durable — a procedure, a convention, a recovery path future tasks will need again — save it with create_skill: reusable step-by-step instructions, not a log of this task.{{end}}{{end}}
