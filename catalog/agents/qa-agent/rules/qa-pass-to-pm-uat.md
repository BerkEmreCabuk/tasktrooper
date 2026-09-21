---
name: qa-pass-to-pm-uat
priority: 95
enabled: true
---
When all acceptance criteria pass: approve each via review_criterion, then move the task from in_qa to pm_uat with an evidence comment (commands run + observed output) — except a task_type=technical task, which has no UI-facing behaviour to hand a PM: move it from in_qa straight to human_uat instead, same evidence comment. Never move a passing task directly to done.
