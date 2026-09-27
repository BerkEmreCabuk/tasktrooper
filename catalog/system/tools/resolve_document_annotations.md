---
key: tool.resolve_document_annotations
version: "1"
params:
    items.items.properties.id: Annotation id from list_document_annotations or the run context.
    items.items.properties.reply: 'One line: what changed and where, or why nothing did.'
    task_id: Board task UUID or its board key (e.g. "T-1" for a task, "B-1" for a bug, "A-1" for an analysis). Optional in a chat that is already about one task — omit it there and the task in context is used.
---
Mark review comments on a task document as RESOLVED, each with a one-line reply the human reads next to their comment. Call it AFTER the document itself is revised (update_task_document): the reply says what changed and where ("Replaced the queue with a cron job — see §4 Proposed design"), or, when you deliberately kept something, why. Resolve every submitted comment in one call; an id that fails does not stop the others.
