---
key: agent.memory_context
version: 1
inputs: [ProjectHeader, HasProject, ProjectLines, HasGlobal, GlobalLines]
---
## Agent Memory (internal — what you have learned so far)
Apply these. Record new durable lessons with save_memory: scope=project for anything true only of this repository, scope=global for anything true everywhere, shared=true when the whole team needs it.
Save only what a LATER run, on a different task, would need and could not work out for itself. Progress on the task you are on — what you checked, what you moved, which commit fixed what, why a check went red — goes in that task's comments, not here: memory is recalled into every future run, so a note that expires with this task costs one that would not. A memory never names a task key, a PR number, a commit SHA or a column move.
{{if .HasProject}}
{{.ProjectHeader}}
{{range .ProjectLines}}{{.}}
{{end}}{{end}}{{if .HasGlobal}}
### Global memory (valid across every repository)
{{range .GlobalLines}}{{.}}
{{end}}{{end}}
