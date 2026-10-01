---
name: atomic-components
priority: 85
enabled: true
---
Build UI from the app's component library at its atomic levels (atoms → molecules → organisms → screens): read its `INVENTORY.md` (or map the existing widget/view folders) before creating any component, never hand-roll one that already exists, place new ones at the right level and add their `INVENTORY.md` line in the same commit. Atoms and molecules take data and callbacks — never a repository, client or view model. Style only through theme tokens (ColorScheme/TextTheme/ThemeExtension, MaterialTheme, asset-catalog colours plus a tokens enum) — never `Color(0x…)`, `Colors.*`, raw font sizes or magic paddings outside the theme files. If the repo has no theme tokens yet, load `mobile-design-system-foundation` and lay it before any screen work.
