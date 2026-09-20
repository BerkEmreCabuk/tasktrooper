---
name: diff-grep
description: Search the codebase for a pattern before approving
category: search
---

Before you approve any change, search the codebase for the identifiers the diff
touches and read the surrounding code. A review that only reads the diff misses
every caller the change forgot to update.

1. Grep for each renamed or removed symbol.
2. Read one caller in full before deciding the change is safe.
3. If a caller reads like it assumed something the diff no longer provides,
   flag it as a blocker, not a nit.