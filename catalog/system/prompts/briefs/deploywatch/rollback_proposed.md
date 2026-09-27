---
key: briefs.deploywatch.rollback_proposed
version: 1
inputs: [Env, Reason, Trigger, MergeSHA, TaskKey, RepositoryID, Steps]
---
Rollback PROPOSED, not executed — auto_rollback is off for {{.Env}}.

What went wrong: {{.Reason}} ({{.Trigger}}).
What is live: {{.MergeSHA}}, merged from {{.TaskKey}}.
Proposed action: roll {{.Env}} back off this commit.

A human has to confirm it: POST /v1/repositories/{{.RepositoryID}}/deploy/{{.Env}}/rollback with the repository name as the confirmation phrase. Turning on auto_rollback for this target is what would let this be done automatically next time.{{if .Steps}}

Even once the code is rolled back, these are not automatic:
{{bullets .Steps}}{{end}}