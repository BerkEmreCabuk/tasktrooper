---
key: guard.out_of_budget_comment
version: 1
inputs: [ErrorMessage, Kept, HasPartial, Partial]
---
Run stopped before finishing — {{.ErrorMessage}}

{{.Kept}}{{if .HasPartial}}

Agent's own summary:

{{.Partial}}{{end}}
