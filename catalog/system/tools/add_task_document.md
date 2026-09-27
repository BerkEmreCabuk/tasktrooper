---
key: tool.add_task_document
version: "1"
params:
    content: 'Document content: markdown, or a full HTML document when format is html'
    format: markdown (default) or html
    task_id: Board task UUID or its board key (e.g. "T-1" for a task, "B-1" for a bug, "A-1" for an analysis).
    title: Document title
---
Add a NEW document to a board task as the current agent — markdown by default, or a self-contained HTML page with `format: "html"` (an analysis report). HTML is sanitized on save: scripts, iframes, forms, event handlers and javascript: URLs are removed; inline <style>, inline SVG and data:/https images are kept. Limit 1 MB. Revising something already attached to the task is update_task_document's job, not this one — never write "Spec v2" next to "Spec". Writing a title that already exists on the task rewrites that document in place rather than duplicating it.
