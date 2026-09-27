---
key: tool.watch_release
version: "1"
params:
    task_id: Board task UUID or its board key (e.g. "T-1" for a task, "B-1" for a bug, "A-1" for an analysis). Optional in a chat that is already about one task — omit it there and the task in context is used.
---
Watch this task's release through its deploy and soak window. While the release is still deploying, verifying, or rolling back, this call PARKS the task and ends the run — that is correct and expected: do not try to poll, wait, or sleep. The release sweeper watches it in the background and re-dispatches this task the moment a verdict is needed (awaiting_verdict) or something failed — you (or the next run) will be woken with the answer. Call it right after merge_task_pull_request for an on_merge release, and right after deploy_release for a dispatch one. Once it returns a result instead of parking, the release has reached a status you must act on: read `next` and follow it (get_release explains each status the same way).
