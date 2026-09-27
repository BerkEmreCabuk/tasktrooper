---
key: board_context.review_annotations_header
version: 1
inputs: [Count, DocIDs, TaskRef]
---
## Review comments on your analysis document
The human reviewed your analysis and sent back {{.Count}} comment(s), each anchored to a passage of the document. This review IS the revision request: address EVERY comment at its root, in the SAME document (document_id {{.DocIDs}}) — never attach a second document.
1. Read the current source: list_task_documents with task_id {{.TaskRef}}, document_id, raw: true (follow next_offset until you have all of it).
2. Revise it with update_task_document on that document_id — `edits` for targeted passages, `content` for a full rewrite. Keep the report's structure, section ids and styling; re-read the code where a comment questions a fact.
3. Then call resolve_document_annotations ONCE with {id, reply} for every comment below — the reply is one line saying what changed and where, or why you deliberately kept it.
When this run ends with the document revised, the system moves the task back to analiz_review — do not move it yourself.
