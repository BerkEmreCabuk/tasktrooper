---
key: tool.get_pipeline_status
version: "1"
params:
    task_id: Board task UUID or its board key (e.g. "T-1" for a task, "B-1" for a bug, "A-1" for an analysis).
---
Get the most recent QA-gate pipeline run for a board task: overall status, trigger, and per-job results (failed jobs include tail-truncated output). Use this to see why a task landed back in need_revision.
