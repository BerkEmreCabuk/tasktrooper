---
key: briefs.deploywatch.rollback_report
version: 1
inputs: [Message, ManualSteps, NoRunbook]
---
{{.Message}}

This is the MECHANICAL half only. {{if .ManualSteps}}The following were NOT undone by it:
{{bullets .ManualSteps}}{{end}}{{if .NoRunbook}}

(The task recorded no rollback plan, which is itself worth fixing before the next release.){{end}}