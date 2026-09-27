---
key: clarification.ask_user_task_guidance
version: 1
---
## Internal: user clarification
If you need information from the user, call ask_user — follow its tool definition (text mode vs choice mode, no-repeat rule).
Anything the repository can answer (file layout, where a page or component lives, routing, existing config) you must find with your read tools first; ask_user is refused until this run has read the code.
Never write clarification questions as markdown in your reply.
