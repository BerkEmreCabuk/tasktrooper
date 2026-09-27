---
key: guard.shell_timeout_retry_below
version: 1
inputs: [Timeout, MaxSeconds]
---
This command needs longer than {{.Timeout}}: re-run it with timeout_seconds up to {{.MaxSeconds}}. Do not repeat it unchanged — it will hit the same wall.
