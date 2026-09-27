---
key: tool.list_document_annotations
version: "1"
params:
    status: Only comments in this status. Omit for all of them.
    task_id: Board task UUID or its board key (e.g. "T-1" for a task, "B-1" for a bug, "A-1" for an analysis). Optional in a chat that is already about one task — omit it there and the task in context is used.
---
READ the review comments a human anchored to passages of a task's documents — the feedback on an analysis report. Each comment has the quoted passage (`quote`, with a little `prefix`/`suffix` of the text around it), the human's `comment`, and a `status`: `submitted` comments are the ones sent back with the latest review and are waiting for you; `resolved` ones you already answered; `open` ones are drafts the human has not sent. Address every submitted comment in the document itself, then answer each with resolve_document_annotations.
