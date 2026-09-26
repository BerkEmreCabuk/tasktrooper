---
key: agent_loop.error_streak_nudge
version: 1
inputs: [Streak, Left]
---
[loop guard] Your last {{.Streak}} tool calls in a row all failed. Changing only the arguments is not working; the method is what is wrong.
Before the next call, do one of these:
1. Read the actual file, directory or command output the failures are about, instead of guessing at paths.
2. Use a different tool for the same goal — write the whole file rather than patching it, list a directory rather than assuming it.
3. If the environment is missing something you need, stop and summarise what is blocked instead of retrying.
{{.Left}} more consecutive failure{{plural .Left "" "s"}} and this run is stopped with the task unfinished.

The failing call's own output follows:

