---
key: tool.record_open_questions
version: "1"
params:
    task_id: Board task UUID or its board key (e.g. "A-1"). Optional in a task run — omit it there and the task in context is used.
    questions.items.properties.prompt: The question itself, as the human will read it.
    questions.items.properties.kind: '"product" or "technical".'
    questions.items.properties.blocking: 'true if the analysis cannot responsibly continue without an answer; false if a reasonable default exists.'
    questions.items.properties.recommended_answer: 'Required when blocking is false: the default you will proceed with if the human leaves it unanswered.'
    update.items.properties.key: 'Which question to edit, e.g. "Q2".'
    withdraw.items: 'Keys of questions to withdraw, e.g. "Q3".'
---
Record open questions on an analiz task for the human to answer on the report page — never type them into the report HTML and never ask them with ask_user on this task type. `questions` ADDS up to 10 new ones; `update` edits a question you already recorded (you cannot change the PROMPT of one the human already answered — withdraw it and add a new one instead); `withdraw` removes questions an answer made moot. Default to non-blocking with your recommended answer; blocking is the exception for something the code and brief genuinely cannot settle. A blocking question still requires exploring first — a question the code already answers is not a question — and the report you attach before ending the run should say as far as you got. Returns the full current list (key, kind, blocking, status, answer).
