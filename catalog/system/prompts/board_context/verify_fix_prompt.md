---
key: board_context.verify_fix_prompt
version: 1
inputs: [FailReport]
---
Automated verification failed in the task workspace. Fix these errors, then re-check your work. Do not post an add_task_comment about the fix or the task being done — the system publishes your closing summary to the card once these checks pass:

{{.FailReport}}
