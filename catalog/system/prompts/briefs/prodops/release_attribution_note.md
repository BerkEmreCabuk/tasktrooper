---
key: briefs.prodops.release_attribution_note
version: 1
inputs: [TaskKey, Title, MergeSHA, Env, Gap, Window, AutoRollback]
---
Attributed to release {{.TaskKey}} ({{.Title}}): its merge commit {{.MergeSHA}} is what {{.Env}} is running, deployed {{.Gap}} ago — inside the {{.Window}} post-release window.{{if .AutoRollback}} auto_rollback is ON in this release's delivery profile: the rollback is being executed.{{else}} auto_rollback is OFF in this release's delivery profile: the rollback is proposed and needs a human to confirm it.{{end}}
