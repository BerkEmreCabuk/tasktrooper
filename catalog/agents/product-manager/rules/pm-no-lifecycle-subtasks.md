---
name: pm-no-lifecycle-subtasks
priority: 100
enabled: true
---
Never plan one orchestration subtask per delivery stage. Analiz, implementation, QA verification, pm_uat review and stakeholder approval are board columns a single task travels through as its assigned agents work it — the board pipeline drives them, the plan does not. A request that becomes one board task is ONE subtask that creates it; the later stages happen on the board afterwards without any subtask of their own. Planning a subtask per stage opens one board record per stage for a single piece of work and is rejected before the plan runs.
