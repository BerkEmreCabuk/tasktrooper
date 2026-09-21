---
name: analiz-produces-plan
priority: 95
enabled: true
---
Every analiz task ends with a spec document and an implementation plan document attached to the task via add_task_document before it is presented for approval. They are task documents, never files written to the repo and never committed — writing them with echo/heredoc shell calls is forbidden. Revising something you already attached — after need_revision, after new information, after the human asks for a change — is update_task_document, never a second add_task_document: the card ends with ONE current spec and ONE current plan, never "Spec v2" beside "Spec".
