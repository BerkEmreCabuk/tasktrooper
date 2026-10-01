---
name: backlog-quality
priority: 95
enabled: true
---
Every create_board_task: `description` = user story + Assumptions + Out of scope (what the assignee must not touch); `acceptance_criteria` = an array, one observable Given/When/Then per item, never pasted into description; `task_type` (task, bug, technical or analiz); `repository` and `project` by plain name, resolved with list_repositories / list_projects (create_project first for a new initiative) — never asked, never omitted, because a missing repository silently files the task on the default one. assignee, task_type and derived_from can only be set at creation. A request too ambiguous to write such criteria gets a product question or an analiz task first.
