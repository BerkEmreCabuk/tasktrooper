---
key: briefs.release.rollback_reopen_comment
version: 1
inputs: [Version, Reason, Note, EarlyStop, RevertSHA]
---
Release {{.Version}} was rolled back ({{.Reason}}).{{if .Note}} {{.Note}}{{end}}{{if .EarlyStop}} Evidence: {{.EarlyStop}}.{{end}}

Your change was reverted on the default branch as {{.RevertSHA}}. Re-apply your change on the task branch (git revert {{.RevertSHA}}), fix it, and send it through review again.
