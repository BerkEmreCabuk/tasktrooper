---
key: tool.update_task_document
version: "1"
params:
    content: The complete new body (markdown, or a full HTML document for an html document). Replaces the existing content entirely. Not together with edits.
    document_id: UUID of the document to rewrite (from list_task_documents). Optional if `title` is given, or if the task has exactly one document.
    edits: Targeted replacements applied in order to the current source. Not together with content.
    edits.items.properties.new_text: Replacement text (may be empty to delete).
    edits.items.properties.old_text: Exact text currently in the document; must occur exactly once.
    format: Change the document's format. Omit to keep the current one.
    new_title: Optional new title. Omit to keep the current one.
    task_id: Board task UUID or its board key (e.g. "T-1" for a task, "B-1" for a bug, "A-1" for an analysis). Optional in a chat that is already about one task — omit it there and the task in context is used.
    title: Title of the document to rewrite, matched exactly (case-insensitive). Ignored when document_id is given.
---
REWRITE a document that already exists on a board task, in place. This is the tool for every revision of a spec, a plan or a report already attached to the task: whoever reads that task must find one current document, not a pile of near-duplicates, so revise instead of adding a "v2". Identify the document by document_id or by its exact title; on a task that has exactly one document both may be omitted. Either `content` REPLACES the whole body — read it first with list_task_documents (raw: true for html) and send the full new text, not a fragment — or `edits` changes passages in place: each old_text must appear exactly once in the current source (for html, the HTML source from list_task_documents raw: true) and is replaced by new_text, in order. The document keeps its format unless `format` is given; html is sanitized on save.
