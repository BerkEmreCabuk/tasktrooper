---
key: tool.list_links
version: "1"
params:
    component: A component's own path (e.g. "apps/web", or "." for a single-purpose repo) to list just that component's links.
    direction: '"out" for what the component calls, "in" for what calls it, "both" (default) for everything.'
    repository_id: Repository UUID. Defaults to the run's own repository; pass this only to look at a different one.
---
List what a component talks to and what talks to it: other components (in this repository or another), and system resources (databases, queues, third-party APIs). Use this before touching an integration point to see who else depends on it.
