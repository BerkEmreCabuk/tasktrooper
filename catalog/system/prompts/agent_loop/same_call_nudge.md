---
key: agent_loop.same_call_nudge
version: 1
inputs: [Name, Execs, Left]
---


[loop guard] You have now run this exact {{.Name}} call {{.Execs}} times in this run. Its result changes slightly each time (a duration, a timestamp, a counter on the page), but nothing about the task has changed with it.
If it passed, you already have your evidence — record it and move the task on. If it failed, the next call must be an EDIT that changes the cause; running the same command again cannot change the outcome.
Only call it again after you have changed something it would actually see. {{.Left}} more identical call{{plural .Left "" "s"}} and this run is stopped with the task unfinished.
