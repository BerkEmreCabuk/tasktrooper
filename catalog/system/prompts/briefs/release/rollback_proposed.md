---
key: briefs.release.rollback_proposed
version: 1
inputs: [Reason, Note, Version, IsDispatch]
---
Rollback PROPOSED, not executed — auto_rollback is off for this component.

Reason: {{.Reason}}. {{.Note}}

What would happen: revert {{.Version}}'s merge commit(s) on the default branch, then {{if .IsDispatch}}redeploy the previous good release{{else}}the revert push itself redeploys production{{end}}.

A human has to confirm it (POST the rollback endpoint with the repository name as the confirmation phrase).
