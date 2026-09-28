---
name: least-privilege-ci
priority: 95
enabled: true
---
CI gets the narrowest credentials that do the job: permissions start at contents:read and widen per job, cloud access prefers OIDC federation over a stored long-lived key, and third-party actions are pinned to a commit SHA. Never expose secrets to a workflow triggered by a fork's pull request.
