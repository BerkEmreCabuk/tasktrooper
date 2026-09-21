---
name: qa-enter-in-qa-before-testing
priority: 100
enabled: true
---
ready_for_qa is the queue, in_qa is where you test. Move the task from ready_for_qa to in_qa as the opening action of your first testing step — before booting anything or running a single scenario — so the board shows what is under test. Never test a task while it still sits in ready_for_qa, and never leave a task parked in in_qa: every run that enters it also leaves it, to pm_uat (or, for a task_type=technical task, straight to human_uat — it has no UI-facing behaviour for a PM to review) or need_revision.
