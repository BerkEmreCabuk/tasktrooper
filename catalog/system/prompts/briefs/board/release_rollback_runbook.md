---
key: briefs.board.release_rollback_runbook
version: 1
inputs: [IncidentTitle, IncidentEnv, IncidentSeverity, IncidentDetail, MergeSHA, Runbook, AutoRollback]
---
ROLLBACK REQUIRED — this task's release is what production is running, and production is unhealthy.

Incident: {{.IncidentTitle}} ({{.IncidentEnv}}, severity {{.IncidentSeverity}})
{{if .IncidentDetail}}{{.IncidentDetail}}
{{end}}Released commit: {{.MergeSHA}}

{{if .Runbook.Empty}}This task recorded NO rollback plan. Say so explicitly when you report — the absence is itself a finding for the next release.

{{else}}{{partial "rollback_runbook" .Runbook}}

{{end}}{{if .AutoRollback}}auto_rollback is ON in this release's delivery profile: call rollback_release with reason=health_incident. It will undo the code — by re-deploying the last good commit where a deploy workflow exists, or by reverting the merge commit on the default branch where the host deploys on push. {{else}}auto_rollback is OFF in this release's delivery profile: call rollback_release with reason=health_incident anyway — it will execute NOTHING and return the written-up proposal (`proposed: true`). That is the correct outcome here. Post what it returns on this task, say plainly that a human has to confirm it, and stop. Do not look for another way to roll production back. {{end}}Then work through the plan above yourself and report every step you performed AND every step you could not — a schema change, a feature flag, anything with a human on the other end. Do not report the rollback as complete unless it is.
