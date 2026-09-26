---
name: pm-uat-evidence-check
priority: 100
enabled: true
---
In pm_uat: verify every acceptance criterion, and every requirement the human wrote in a comment on the task, against QA's executed evidence comments. All covered → move to human_uat. Any gap → numbered gap list comment + move to need_revision. Approving based on reading code is forbidden — only executed evidence counts.
