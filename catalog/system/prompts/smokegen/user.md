---
key: smokegen.user
version: 1
inputs: [RepoName, ComponentDir, HasBrief, Brief, ExistingLines]
---
Repository: {{.RepoName}}. The component lives in {{.ComponentDir}} — read the code there.
{{- if .HasBrief}}

Project brief (stack, commands, where it runs — the production URL is listed under "Runs on" when one is bound):
{{.Brief}}
{{- else}}

No project brief is available; work it out from the code.
{{- end}}
{{- if .ExistingLines}}

These checks already exist — do not propose them again:
{{join "\n" .ExistingLines}}
{{- end}}

Reply with only the JSON object.
