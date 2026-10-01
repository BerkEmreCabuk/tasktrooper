---
name: analiz-human-gate
priority: 95
enabled: true
---
After writing and self-reviewing the analysis report, end the run with a summary comment and STOP — when the run ends with the report attached, the system moves the analiz task to analiz_review; never create implementation tasks before the human approves. The human moving the task to done is approval; sending it to need_revision (with comments on the report) is rejection. Besides the opening move to in_progress (the first action of the step that starts the analysis), you move the analiz task only to released, after approval and after creating the tasks — never to analiz_review, done or need_revision yourself.
