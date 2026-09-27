---
key: tool.list_task_documents
version: "1"
params:
    document_id: Only this document (UUID). Required with `offset` when a task has more than one document.
    limit: Maximum characters of content per document. Defaults to 12000 for raw html, unlimited otherwise; at most 100000.
    offset: Character offset to start the returned content at (from a previous call's next_offset).
    raw: Return html documents as their HTML source instead of the text rendition. Needed to revise one.
    task_id: Board task UUID or its board key (e.g. "T-1" for a task, "B-1" for a bug, "A-1" for an analysis). Optional in a chat that is already about one task — omit it there and the task in context is used.
---
READ the documents attached to a board task, in order. This is where an analysis lives: the analysis report an analiz task produced is a document on THAT task, never a file in the repository, so pass the analiz task's key (e.g. "A-12") to read it. Your own task's `relations` name it with relation_type "derived_from" — and the same documents are already in your run context, so use this to re-read a long plan or to look at an analysis you were not handed. An html document (`format: "html"`) comes back as readable TEXT by default; pass `raw: true` to get its HTML source — which is what you need before revising it with update_task_document. Raw HTML is returned in windows of `limit` characters (default 12000): when `next_offset` is present, call again with that `offset` and the same `document_id` for the rest. This tool only reads; add_task_document is what writes one.
