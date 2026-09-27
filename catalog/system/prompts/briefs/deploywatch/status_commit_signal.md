---
key: briefs.deploywatch.status_commit_signal
version: 1
inputs: [Who, State, SHA, Description]
---
{{.Who}} {{if eq .State "success"}}reported this deploy successful{{else if eq .State "failure"}}reported this deploy FAILED{{else}}has not finished this deploy yet{{end}} for {{.SHA}} (no Actions deploy job exists — this repository deploys on push).{{if .Description}} {{.Description}}{{end}}
