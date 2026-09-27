---
key: briefs.prodops.remedy.rollback_summary_recent
version: 1
inputs: [Env, Gap]
---
A deploy to {{.Env}} finished {{.Gap}} before this incident started — treat it as the cause and roll back first, diagnose after.
