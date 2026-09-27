---
key: briefs.deploywatch.status_nothing_reports_deploy
version: 1
inputs: [SHA]
---
Nothing reports a deploy of {{.SHA}}: no Actions run carries a deploy job for it, and no commit status or GitHub Deployment was written against it. This repository does not deploy on merge (or its deploy has not started yet and has left no trace).
