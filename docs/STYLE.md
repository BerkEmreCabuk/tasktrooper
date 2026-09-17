# Writing docs

Every page under `docs/` is markdown with YAML frontmatter, rendered on
tasktrooper.ai/docs and readable on GitHub. `manifest.json` is the sidebar.

```
---
title: Board and columns
description: One sentence shown under the title and in search results.
---
```

- Write for a person using the app, not for a contributor. Say what the screen
  or setting is called and where it is (Settings → Agents → …). Internals only
  when they explain behaviour the user sees.
- Facts come from the code and the internal docs (`server/.ai/*.md`,
  `server/README.md`, `desktop/README.md`). Do not invent flags, paths, limits
  or defaults; if a value is configurable, say where.
- Lead with what it does, then how to use it, then the edge cases. Short
  sections, real headings, tables for options, fenced blocks for commands.
- Link between pages with relative links: `[Tool policies](tool-policies.md)`.
  The site rewrites them.
- No marketing, no "powerful", no "seamless". No emoji.
