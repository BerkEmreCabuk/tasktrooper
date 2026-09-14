---
name: platform-permissions
category: mobile
description: Platform permission handling
---

# Platform Permissions

- Request permissions at point of use with a clear rationale — never a wall of prompts at first launch.
- Handle every state: granted, denied, restricted, and "denied, don't ask again" (deep-link to settings with an explanation).
- The feature degrades gracefully without the permission; a denial never crashes or dead-ends the flow.
- Test the denied path explicitly — it is the path reviewers and stores check first.
