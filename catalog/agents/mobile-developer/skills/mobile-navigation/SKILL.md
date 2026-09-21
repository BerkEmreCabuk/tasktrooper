---
name: mobile-navigation
category: mobile
description: Mobile navigation patterns
---
# Mobile Navigation

- Stack navigators for drill-in flows, tab navigators for top-level sections; don't nest deeper than the user can reason about.
- Deep-link critical screens; navigation params carry IDs, not whole objects.
- Preserve back-stack behavior on Android: hardware back must do what the visible back affordance does.
- Reset navigation state intentionally (after logout, after completing a flow) — never leave stale stacks behind auth boundaries.
