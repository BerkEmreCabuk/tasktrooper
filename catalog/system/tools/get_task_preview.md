---
key: tool.get_task_preview
version: "1"
params:
    task_id: Board task UUID or its board key (e.g. "T-1" for a task, "B-1" for a bug, "A-1" for an analysis). Optional in a chat that is already about one task — omit it there and the task in context is used.
---
Find the task branch's per-branch preview deployment (Vercel builds every pushed branch / pull request as its own preview). Per component: status (ready, building, error, canceled, none), the stable branch_url and this build's url, the commit it was built from and built_from_pr_head, and — for a preview behind Vercel Deployment Protection — open_url (for the browser; it sets a bypass cookie) and request_headers (send them on every HTTP request). A preview is the task's own code only when status is ready AND built_from_pr_head is true. Read-only; it never deploys anything.
