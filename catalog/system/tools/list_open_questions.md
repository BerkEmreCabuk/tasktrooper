---
key: tool.list_open_questions
version: "1"
params:
    task_id: Board task UUID or its board key (e.g. "A-1"). Optional in a task run — omit it there and the task in context is used.
---
READ every open question recorded on an analiz task, withdrawn ones included, each with its kind, whether it is blocking, its status and the human's answer (or recommended answer, for an unanswered non-blocking one). Call it after `resumed: questions_answered` or when the run context shows new answers, to see every question's current state before continuing the report from where it stopped.
