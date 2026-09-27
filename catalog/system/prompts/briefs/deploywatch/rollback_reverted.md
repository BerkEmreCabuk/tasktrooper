---
key: briefs.deploywatch.rollback_reverted
version: 1
inputs: [Env, MergeSHA, RevertSHA]
---
Rolled back {{.Env}} by reverting {{.MergeSHA}} on the default branch and pushing ({{.RevertSHA}}). This repository has no deploy workflow — it deploys on push, so the revert commit IS the rollback deploy.
