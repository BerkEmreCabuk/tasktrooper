---
key: guard.release_rollback_not_allowed
version: 1
inputs: [Why]
---
rollback_release only applies to a failed or awaiting-verdict release, or one released within the last 24h that is still its component's newest ({{.Why}})
