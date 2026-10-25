---
name: atomic-components
priority: 85
enabled: true
---
Build UI from the atomic library (`ui → atoms → molecules → organisms → templates → pages`): read `src/components/INVENTORY.md` and the level folders before creating any component, never hand-roll one that already exists, place new ones at the right level and add their `INVENTORY.md` line in the same commit. Style only through design tokens, never raw palette classes or hex values. If the repo has no design system yet, load `web-design-system-foundation` and lay it before any page work.
