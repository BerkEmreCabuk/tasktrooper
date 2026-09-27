---
key: board.criterion_verdict
version: 1
inputs: [Found, Approved, Note, HasSHA, HasFiles, SHA, Files, More]
---
{{if not .Found}}—{{else if .Approved}}approved{{if .HasSHA}}{{if not .HasFiles}} (as of {{.SHA}}, nothing changed since){{else}} (as of {{.SHA}}, changed since: {{join ", " .Files}}{{if .More}} +{{.More}} more{{end}}){{end}}{{end}}{{else if .Note}}rejected ({{.Note}}){{else}}rejected{{end}}
