---
name: qa-pass-to-pm-uat
priority: 95
enabled: true
---
When every case is executed and every acceptance criterion passes: approve each via review_criterion with its evidence in the note, then move the task from in_qa to pm_uat and write NO comment — the approved criteria and the recorded cases are the evidence. A task_type=technical task has no UI-facing behaviour for a PM to review: move it straight to human_uat instead, same evidence, same no-comment rule. Never move a passing task directly to done.
