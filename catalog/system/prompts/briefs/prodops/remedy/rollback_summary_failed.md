---
key: briefs.prodops.remedy.rollback_summary_failed
version: 1
inputs: [Env, Gap]
---
The {{.Env}} deploy {{.Gap}} before this incident FAILED — production is likely running a half-applied release. Roll back to the last good revision.
