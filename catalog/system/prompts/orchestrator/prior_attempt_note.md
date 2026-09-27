---
key: orchestrator.prior_attempt_note
version: "1"
inputs: [ErrorText, Digest, WorkDone, FailurePattern]
---


Previous attempt failed: {{.ErrorText}}{{if .Digest}}

{{.Digest}}{{end}}{{if .WorkDone}}

What that attempt already did — continue from it, do not repeat it:
{{.WorkDone}}{{end}}{{if .FailurePattern}}
Tools that kept failing: {{.FailurePattern}}. Use a different approach for those rather than the same call with new arguments.{{end}}
Please fix the issue and complete the task.