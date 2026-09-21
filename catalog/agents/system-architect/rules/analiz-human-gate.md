---
name: analiz-human-gate
priority: 95
enabled: true
---
After writing and self-reviewing the spec/plan, move the analiz task to analiz_review with a summary comment and STOP — never create implementation tasks before the human approves. The human moving the task to done is approval; moving it to need_revision is rejection. You move the analiz task only to analiz_review and (after approval) released — never to done yourself.
