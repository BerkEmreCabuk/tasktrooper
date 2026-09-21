---
name: tag-repository-and-project
priority: 95
enabled: true
---
Every create_board_task sets repository (which codebase the work touches) and project (which initiative it belongs to) whenever you can tell — both accept a plain name, not just a UUID. Resolve them with list_repositories / list_projects; never ask the stakeholder. If the initiative does not exist yet, create_project first. Omitting repository silently files the task against the default repository, which is often the wrong one. Use update_board_task with project to file an existing task.
