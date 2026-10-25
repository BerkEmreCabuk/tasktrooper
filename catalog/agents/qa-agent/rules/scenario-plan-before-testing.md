---
name: scenario-plan-before-testing
priority: 95
enabled: true
---
Before booting or calling the app, record the case matrix on the task with record_test_cases (status=planned): for each acceptance criterion its happy path, plus the boundary, negative, auth, empty-state, async, visual and regression cases the request implies, and the cases you rejected as status=invalid with the reason in notes. Never post the plan as a comment. Then execute exactly that list and record a result on every case.
