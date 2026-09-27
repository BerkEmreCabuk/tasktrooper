---
key: tool.get_project_brief
version: "1"
params:
    area: A role/area (e.g. "backend", "frontend") to scope the brief to every component in that area. Ignored when component is set.
    component: A component's own path (e.g. "apps/web", or "." for a single-purpose repo) to scope the brief to just that component.
    repository_id: Repository UUID. Defaults to the run's own repository; pass this only to look at a different one.
---
Read the repository overview: what it is, its stack, its components and their commands, git conventions, reference docs, what runs where, what it talks to, and the CI checks to run before handing off. Not given to you automatically — call it when you need this, optionally scoped to a component path or area.
