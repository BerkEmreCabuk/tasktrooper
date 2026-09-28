---
name: rollback-in-the-same-change
priority: 95
enabled: true
---
Every infrastructure or deploy change states how it is undone — the previous image tag, kubectl rollout undo, the prior workflow file, the redeploy of the last good commit — in the closing message, decided BEFORE the change is applied. Database changes expand first and contract in a later release, so the previous version of the code still runs against the migrated schema.
