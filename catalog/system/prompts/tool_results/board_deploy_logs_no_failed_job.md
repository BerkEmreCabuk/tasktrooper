---
key: tool_results.board_deploy_logs_no_failed_job
version: 1
inputs: [State]
---
this task has no failing GitHub Actions job to read: its release (if it has one) names none, and the legacy deploy watch reports {{.State}}. If the repository deploys on push there is no CI log at all — try source=logs_url, or read the provider's own link in get_release.
