---
key: tool.get_deploy_logs
version: "1"
params:
    env: Which environment's logs_url to read. Defaults to prod. Only used with source=logs_url.
    job_id: Actions job id, from get_release's release.deploy.failed_job. Omit to use this task's failing deploy job.
    source: actions_job (the deploy job's CI log, default) or logs_url (the application's own log endpoint)
    task_id: Board task UUID or its board key (e.g. "T-1" for a task, "B-1" for a bug, "A-1" for an analysis). Optional in a chat that is already about one task — omit it there and the task in context is used.
---
Read the log behind a deploy. Two sources: `actions_job` (default) fetches the failing GitHub Actions deploy job's log — pass the `job_id` get_release reported under `release.deploy.failed_job`, or omit it and the failing job of this task's release is used (falling back to the legacy per-task deploy watch when the task has no release); `logs_url` fetches the environment's own log endpoint, if one is recorded on the deploy target. The result is a SUMMARY, not a dump: the error-looking lines are lifted out first and the tail follows them, so paste the relevant part into your comment rather than the whole thing. Use it on a failed deploy before rolling back, and on a successful one whose environment then went unhealthy.
