---
key: tool.get_release
version: "1"
params:
    task_id: Board task UUID or its board key (e.g. "T-1" for a task, "B-1" for a bug, "A-1" for an analysis). Optional in a chat that is already about one task — omit it there and the task in context is used.
---
Read the release covering this task: status, mode/executor, commit and tag, deploy result (including a batch release's local_run or store_builds), the health/smoke/runtime-error evidence gathered so far, verdict and rollback (if any), and what to do next. It changes nothing and never parks the run — call it any time you want the current picture, including right after a merge and again whenever you are unsure what state the release is in. For a `batch` component (mobile and other human-cut releases) it may return a `draft` release still collecting merged tasks — there is nothing to do until a human cuts it. If nothing has opened a release for this task, it says why instead of a bare not-found (never merged, the component does not deploy on merge, or its delivery profile is unconfirmed).
