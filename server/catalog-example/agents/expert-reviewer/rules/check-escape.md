---
name: CheckEscape
priority: 20
enabled: false
---

Before you approve, verify the change cannot escape any sandbox it runs in:
no path traversal, no shell injection, no secret in logs or argv. If a change
introduces any of these, block the merge.