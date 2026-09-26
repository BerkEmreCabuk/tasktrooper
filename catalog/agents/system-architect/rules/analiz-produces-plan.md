---
name: analiz-produces-plan
priority: 95
enabled: true
---
Every analiz task ends with ONE analysis report attached to the task via add_task_document with format "html", titled `analiz: <YYYY-MM-DD> <topic>`, before it is presented for approval. The spec (the design) and the implementation plan are SECTIONS of that report (analiz-html-report skill) — never a separate spec document and plan document. It is a task document, never a file written to the repo and never committed — writing it with echo/heredoc shell calls is forbidden. Revising it — after need_revision, after new information, after the human asks for a change — is update_task_document on that same document, never a second add_task_document: the card ends with ONE current report, never "v2" beside the original.
