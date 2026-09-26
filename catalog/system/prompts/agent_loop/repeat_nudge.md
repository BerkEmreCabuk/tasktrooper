---
key: agent_loop.repeat_nudge
version: 1
inputs: [Name, Count, Left]
---
[loop guard] You have now made this exact {{.Name}} call {{.Count}} times and it returned the same result every time. That result is no longer shown to you, and the call is no longer being run — asking again gains nothing.
Empty output is not a failure: sed, mv, cp and mkdir print nothing when they succeed.
Do one of these instead:
1. Read the file or state this call was meant to change, and continue from what you find.
2. Change the method — write the whole file instead of editing it in place, or use a different tool.
3. If neither is possible, stop calling tools and summarise what you changed and what is blocked.
{{.Left}} more identical call{{plural .Left "" "s"}} and this run is stopped with the task unfinished.
